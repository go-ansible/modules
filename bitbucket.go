package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// Bitbucket's four modules in community.general all sit on one helper,
// `module_utils/_bitbucket.py`'s BitbucketHelper, and differ only in the
// endpoint they drive and how they decide a change is needed. That shape
// is kept here: one client reproducing the helper, four modules over it.
//
// Every request goes through curl ON THE TARGET, the pattern the other
// HTTP modules in this package use, with the url, the headers and the
// body on curl's stdin config. That matters more than usual here: the
// OAuth client secret, the basic-auth password, the bearer token and a
// pipeline variable's value are all credentials, and none of them
// reaches argv. TestBitbucketKeepsSecretsOffArgv holds that, and carries
// its own control -- each secret must be found on STDIN -- so a test
// that stopped looking would not pass by looking at nothing.
//
// # What a request can and cannot say
//
// Real's failure messages interpolate fetch_url's `info` dict wholesale:
// `Failed to create deploy key `x`: {'status': 400, 'msg': ...}`. That
// dict is made by urllib and Ansible's url helper, not by Bitbucket, and
// there is no way to produce its text from curl. Each message below
// keeps real's sentence word for word and carries the HTTP status and
// Bitbucket's own trimmed body in place of the dict.

const bitbucketAPIURL = "https://api.bitbucket.org"

// bitbucketOAuthURL is where the helper trades a client id and secret
// for a bearer token. It is NOT under api.bitbucket.org, which is why it
// is spelled out rather than built from bitbucketAPIURL.
const bitbucketOAuthURL = "https://bitbucket.org/site/oauth2/access_token"

// bitbucketClient is BitbucketHelper: the credentials for one module
// run, plus the one request method all four modules use.
type bitbucketClient struct {
	conn remoteexec.Connection

	user     string
	password string
	// accessToken is set only when client_id and client_secret were
	// given. When it is set it WINS over user/password, which is the
	// helper's own order.
	accessToken string
}

// bitbucketAuth builds the client, reproducing the helper's
// argument_spec, required_one_of and required_together, and then
// fetching an access token when a client id and secret were given.
//
// The environment fallbacks are read ON THE TARGET, through the
// connection. Real's env_fallback is evaluated inside the module
// process, which runs on the target, so that is where the variable has
// to be looked for: reading the control node's environment instead would
// quietly use the operator's own credentials for a host that was meant
// to supply its own, and would ignore a play's `environment:`.
func bitbucketAuth(ctx context.Context, conn remoteexec.Connection, args map[string]any, module string) (*bitbucketClient, Result, bool) {
	clientID, err := bitbucketParam(ctx, conn, args, "client_id", "", "BITBUCKET_CLIENT_ID")
	if err != nil {
		return nil, Fail(module + ": " + err.Error()), false
	}
	clientSecret, err := bitbucketParam(ctx, conn, args, "client_secret", "", "BITBUCKET_CLIENT_SECRET")
	if err != nil {
		return nil, Fail(module + ": " + err.Error()), false
	}
	user, err := bitbucketParam(ctx, conn, args, "user", "username", "BITBUCKET_USERNAME")
	if err != nil {
		return nil, Fail(module + ": " + err.Error()), false
	}
	password, err := bitbucketParam(ctx, conn, args, "password", "", "BITBUCKET_PASSWORD")
	if err != nil {
		return nil, Fail(module + ": " + err.Error()), false
	}

	// required_one_of=[['client_id', 'client_secret', 'user', 'password']]
	if clientID == "" && clientSecret == "" && user == "" && password == "" {
		return nil, Fail("one of the following is required: client_id, client_secret, user, password"), false
	}
	// required_together=[['client_id', 'client_secret'], ['user', 'password']]
	if (clientID == "") != (clientSecret == "") {
		return nil, Fail("parameters are required together: client_id, client_secret"), false
	}
	if (user == "") != (password == "") {
		return nil, Fail("parameters are required together: user, password"), false
	}

	c := &bitbucketClient{conn: conn, user: user, password: password}
	if clientID == "" {
		return c, Result{}, true
	}

	// The helper's own token exchange: basic auth with the client id and
	// secret, and a form body of exactly grant_type=client_credentials.
	body, status, err := curlDo(ctx, conn, curlReq{
		Method:    "POST",
		URL:       bitbucketOAuthURL,
		Form:      [][2]string{{"grant_type", "client_credentials"}},
		BasicAuth: clientID + ":" + clientSecret,
	})
	if err != nil {
		return nil, Fail(module + ": " + err.Error()), false
	}
	if status != 200 {
		return nil, Fail(fmt.Sprintf("Failed to retrieve access token: HTTP %d %s",
			status, strings.TrimSpace(body))), false
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(body), &token); err != nil || token.AccessToken == "" {
		return nil, Fail(fmt.Sprintf("Failed to retrieve access token: HTTP %d %s",
			status, strings.TrimSpace(body))), false
	}
	c.accessToken = token.AccessToken
	return c, Result{}, true
}

// bitbucketParam reads one credential argument, falling back to an
// environment variable ON THE TARGET when the playbook did not give it.
// alias is the second name the argument answers to, or "" when it has
// none -- `user` is aliased to `username`, the others are not.
func bitbucketParam(ctx context.Context, conn remoteexec.Connection, args map[string]any, name, alias, envVar string) (string, error) {
	var v string
	if alias != "" {
		v = argStringAliased(args, name, alias, "")
	} else {
		v = argString(args, name, "")
	}
	if v != "" {
		return v, nil
	}
	return targetEnv(ctx, conn, envVar)
}

// bitbucketRequest is the helper's `request`: a bearer token when there
// is one, basic auth otherwise, a JSON body when one is given, and the
// decoded response alongside the status.
//
// An empty response body decodes to an empty map rather than an error,
// which is what the helper does: a 204 from a DELETE carries nothing.
func (c *bitbucketClient) request(ctx context.Context, method, apiURL string, body any) (map[string]any, int, error) {
	req := curlReq{Method: method, URL: apiURL}
	switch {
	case c.accessToken != "":
		req.Headers = [][2]string{{"Authorization", "Bearer " + c.accessToken}}
	case c.user != "" && c.password != "":
		req.BasicAuth = c.user + ":" + c.password
	}
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		req.JSON = string(encoded)
		req.Headers = append(req.Headers, [][2]string{{"Content-Type", "application/json"}}...)
	}

	raw, status, err := curlDo(ctx, c.conn, req)
	if err != nil {
		return nil, 0, err
	}
	content := map[string]any{}
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &content); err != nil {
			// Not an error on its own: a non-JSON body from an error
			// status is normal, and the caller reports the status.
			content = map[string]any{}
		}
	}
	return content, status, nil
}

// bitbucketValues pulls the `values` array out of a paginated response
// as a slice of objects, skipping anything that is not one.
func bitbucketValues(content map[string]any) []map[string]any {
	raw, _ := content["values"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// bitbucketRepoArgs reads the two arguments every one of the four takes.
func bitbucketRepoArgs(args map[string]any) (workspace, repository string, err error) {
	repository, err = requireString(args, "repository")
	if err != nil {
		return "", "", err
	}
	workspace, err = requireString(args, "workspace")
	if err != nil {
		return "", "", err
	}
	return workspace, repository, nil
}

// bitbucketState reads the `state` argument, which is required on all
// four and takes only present or absent.
func bitbucketState(args map[string]any) (string, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return "", err
	}
	if state != "present" && state != "absent" {
		return "", errArg("value of state must be one of: present, absent, got: %s", state)
	}
	return state, nil
}

// moduleBitbucketAccessKey implements Ansible's `bitbucket_access_key`
// (community.general) module: manages a repository's deploy keys.
//
// Args: repository (required); workspace (required); label (required);
// state (required, present/absent); key; plus the four credential
// arguments client_id, client_secret, user (alias username) and
// password.
//
// # The comparison is a PREFIX test, not equality
//
// When a key with the label already exists, real updates it only if the
// NEW key does not start with the stored one -- `key.startswith(existing)`
// -- rather than comparing the two. That is deliberate upstream and is
// kept: Bitbucket stores the key without its trailing comment, so an
// equality test would rewrite the key on every run for anyone whose
// `ssh-rsa AAAA... user@host` carries one.
//
// Bitbucket has no update for a key under the same label, so an update
// is a delete followed by a create, as real does it.
func moduleBitbucketAccessKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	workspace, repository, err := bitbucketRepoArgs(args)
	if err != nil {
		return Result{}, err
	}
	label, err := requireString(args, "label")
	if err != nil {
		return Result{}, err
	}
	state, err := bitbucketState(args)
	if err != nil {
		return Result{}, err
	}
	key := argString(args, "key", "")
	if key == "" && state == "present" {
		return Fail("`key` is required when the `state` is `present`"), nil
	}

	if res, ok := requireCurl(ctx, conn, "bitbucket_access_key", "Bitbucket"); !ok {
		return res, nil
	}
	c, res, ok := bitbucketAuth(ctx, conn, args, "bitbucket_access_key")
	if !ok {
		return res, nil
	}

	listURL := fmt.Sprintf("%s/2.0/repositories/%s/%s/deploy-keys/", bitbucketAPIURL, workspace, repository)
	detailURL := func(id any) string {
		return fmt.Sprintf("%s/2.0/repositories/%s/%s/deploy-keys/%v", bitbucketAPIURL, workspace, repository, id)
	}

	existing, res, ok := bitbucketFindPaged(ctx, c, listURL, "label", label,
		"Failed to retrieve the list of deploy keys")
	if !ok {
		return res, nil
	}

	create := func() (Result, bool) {
		_, status, err := c.request(ctx, "POST", listURL, map[string]any{"key": key, "label": label})
		if err != nil {
			return Fail("bitbucket_access_key: " + err.Error()), false
		}
		switch status {
		case 404:
			return Fail("Invalid `repository` or `workspace`"), false
		case 403:
			return Fail("OAuth consumer `client_id` should have permissions to read and administrate the repository"), false
		case 400:
			return Fail("Invalid SSH key or key is already in use"), false
		case 200:
			return Result{}, true
		}
		return Fail(fmt.Sprintf("Failed to create deploy key `%s`: HTTP %d", label, status)), false
	}
	remove := func(id any) (Result, bool) {
		_, status, err := c.request(ctx, "DELETE", detailURL(id), nil)
		if err != nil {
			return Fail("bitbucket_access_key: " + err.Error()), false
		}
		switch status {
		case 404:
			return Fail("Invalid `repository` or `workspace`"), false
		case 403:
			return Fail("OAuth consumer `client_id` should have permissions to read and administrate the repository"), false
		case 204:
			return Result{}, true
		}
		return Fail(fmt.Sprintf("Failed to delete deploy key `%s`: HTTP %d", label, status)), false
	}

	switch {
	case existing == nil && state == "present":
		if !InCheckMode(args) {
			if res, ok := create(); !ok {
				return res, nil
			}
		}
		return Changed(""), nil

	case existing != nil && state == "present":
		stored, _ := existing["key"].(string)
		if strings.HasPrefix(key, stored) {
			return Ok(""), nil
		}
		if !InCheckMode(args) {
			if res, ok := remove(existing["id"]); !ok {
				return res, nil
			}
			if res, ok := create(); !ok {
				return res, nil
			}
		}
		return Changed(""), nil

	case existing != nil && state == "absent":
		if !InCheckMode(args) {
			if res, ok := remove(existing["id"]); !ok {
				return res, nil
			}
		}
		return Changed(""), nil
	}
	return Ok(""), nil
}

// bitbucketFindPaged walks a `next`-paginated collection looking for the
// one object whose field equals want.
//
// The loop follows the server's own `next` link rather than counting
// pages, which is what real does for deploy keys and known hosts. A page
// without a `next` ends it; a missing match returns nil.
func bitbucketFindPaged(ctx context.Context, c *bitbucketClient, firstURL, field, want, failMsg string) (map[string]any, Result, bool) {
	next := firstURL
	for next != "" {
		content, status, err := c.request(ctx, "GET", next, nil)
		if err != nil {
			return nil, Fail(err.Error()), false
		}
		switch status {
		case 404:
			return nil, Fail("Invalid `repository` or `workspace`."), false
		case 403:
			return nil, Fail("OAuth consumer `client_id` should have permissions to read and administrate the repository"), false
		case 200:
		default:
			return nil, Fail(fmt.Sprintf("%s: HTTP %d", failMsg, status)), false
		}
		for _, v := range bitbucketValues(content) {
			if s, _ := v[field].(string); s == want {
				return v, Result{}, true
			}
		}
		next, _ = content["next"].(string)
	}
	return nil, Result{}, true
}

// moduleBitbucketPipelineKeyPair implements Ansible's
// `bitbucket_pipeline_key_pair` (community.general) module: sets or
// removes the SSH key pair a repository's pipelines run with.
//
// Args: repository (required); workspace (required); state (required,
// present/absent); public_key; private_key; plus the four credential
// arguments.
//
// There is one key pair per repository, so there is no list to page
// through and no identifier: a GET returns it or 404s. A present state
// compares only the PUBLIC key, because Bitbucket never returns the
// private one -- which means a changed private key alone is invisible to
// this module, in real exactly as here.
func moduleBitbucketPipelineKeyPair(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	workspace, repository, err := bitbucketRepoArgs(args)
	if err != nil {
		return Result{}, err
	}
	state, err := bitbucketState(args)
	if err != nil {
		return Result{}, err
	}
	publicKey := argString(args, "public_key", "")
	privateKey := argString(args, "private_key", "")
	if (publicKey == "" || privateKey == "") && state == "present" {
		return Fail("`public_key` and `private_key` are required when the `state` is `present`"), nil
	}

	if res, ok := requireCurl(ctx, conn, "bitbucket_pipeline_key_pair", "Bitbucket"); !ok {
		return res, nil
	}
	c, res, ok := bitbucketAuth(ctx, conn, args, "bitbucket_pipeline_key_pair")
	if !ok {
		return res, nil
	}

	apiURL := fmt.Sprintf("%s/2.0/repositories/%s/%s/pipelines_config/ssh/key_pair", bitbucketAPIURL, workspace, repository)

	content, status, err := c.request(ctx, "GET", apiURL, nil)
	if err != nil {
		return Fail("bitbucket_pipeline_key_pair: " + err.Error()), nil
	}
	// 404 means the account, the repository OR the key pair is absent,
	// and real does not tell them apart here: it treats all three as "no
	// key pair", and lets the write that follows produce the error.
	var existing map[string]any
	if status != 404 {
		existing = content
	}

	switch {
	case state == "present" && (existing == nil || existingPublicKey(existing) != publicKey):
		if !InCheckMode(args) {
			_, status, err := c.request(ctx, "PUT", apiURL, map[string]any{
				"private_key": privateKey,
				"public_key":  publicKey,
			})
			if err != nil {
				return Fail("bitbucket_pipeline_key_pair: " + err.Error()), nil
			}
			if status == 404 {
				return Fail("Account or repository was not found"), nil
			}
			if status != 200 {
				return Fail(fmt.Sprintf("Failed to create or update pipeline ssh key pair : HTTP %d", status)), nil
			}
		}
		return Changed(""), nil

	case state == "absent" && existing != nil:
		if !InCheckMode(args) {
			_, status, err := c.request(ctx, "DELETE", apiURL, nil)
			if err != nil {
				return Fail("bitbucket_pipeline_key_pair: " + err.Error()), nil
			}
			if status == 404 {
				return Fail("Account or repository was not found"), nil
			}
			if status != 204 {
				return Fail(fmt.Sprintf("Failed to delete pipeline ssh key pair: HTTP %d", status)), nil
			}
		}
		return Changed(""), nil
	}
	return Ok(""), nil
}

func existingPublicKey(m map[string]any) string {
	s, _ := m["public_key"].(string)
	return s
}

// moduleBitbucketPipelineKnownHost implements Ansible's
// `bitbucket_pipeline_known_host` (community.general) module: adds a
// host to the SSH known hosts a repository's pipelines trust.
//
// Args: repository (required); workspace (required); name (required, the
// hostname); state (required, present/absent); key; plus the four
// credential arguments.
//
// # Where the host key comes from when `key` is not given
//
// Real opens a raw socket to port 22 and asks paramiko for the key the
// server negotiates. This port runs `ssh-keyscan` ON THE TARGET instead,
// which is the substitution this package makes for a Python library
// everywhere else: a standard tool over an SDK.
//
// The divergence is stated rather than hidden. ssh-keyscan returns every
// key type the host offers and this takes the first; paramiko returns
// the single type its own algorithm negotiation picked. For a host
// offering several, the two can register a DIFFERENT TYPE of key for the
// same host. Either is a valid known-host entry and either lets the
// pipeline connect, and the module only writes when no entry for the
// hostname exists at all, so the choice cannot cause a rewrite loop --
// but the stored key_type may differ from real's, and a test asserting
// on it would see that.
//
// A `key` given explicitly must carry its type: real splits on the first
// space and fails with "Public key type is unknown" when there is none.
func moduleBitbucketPipelineKnownHost(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	workspace, repository, err := bitbucketRepoArgs(args)
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state, err := bitbucketState(args)
	if err != nil {
		return Result{}, err
	}

	if res, ok := requireCurl(ctx, conn, "bitbucket_pipeline_known_host", "Bitbucket"); !ok {
		return res, nil
	}
	c, res, ok := bitbucketAuth(ctx, conn, args, "bitbucket_pipeline_known_host")
	if !ok {
		return res, nil
	}

	listURL := fmt.Sprintf("%s/2.0/repositories/%s/%s/pipelines_config/ssh/known_hosts/", bitbucketAPIURL, workspace, repository)

	existing, res, ok := bitbucketFindPaged(ctx, c, listURL, "hostname", name,
		"Failed to retrieve list of known hosts")
	if !ok {
		return res, nil
	}

	switch {
	case existing == nil && state == "present":
		if !InCheckMode(args) {
			keyType, key, res, ok := bitbucketHostKey(ctx, conn, args, name)
			if !ok {
				return res, nil
			}
			_, status, err := c.request(ctx, "POST", listURL, map[string]any{
				"hostname": name,
				"public_key": map[string]any{
					"key_type": keyType,
					"key":      key,
				},
			})
			if err != nil {
				return Fail("bitbucket_pipeline_known_host: " + err.Error()), nil
			}
			if status == 404 {
				return Fail("Account or repository was not found"), nil
			}
			if status != 201 {
				// Real's own sentence here reads
				// `module.params['hostname']`, but the argument is called
				// `name` and there is no `hostname` key -- so this branch
				// raises a KeyError in real instead of printing anything.
				// The sentence is kept with the argument it meant.
				return Fail(fmt.Sprintf("Failed to create known host `%s`: HTTP %d", name, status)), nil
			}
		}
		return Changed(""), nil

	case existing != nil && state == "absent":
		if !InCheckMode(args) {
			uuid, _ := existing["uuid"].(string)
			_, status, err := c.request(ctx, "DELETE",
				fmt.Sprintf("%s/2.0/repositories/%s/%s/pipelines_config/ssh/known_hosts/%s",
					bitbucketAPIURL, workspace, repository, uuid), nil)
			if err != nil {
				return Fail("bitbucket_pipeline_known_host: " + err.Error()), nil
			}
			if status == 404 {
				return Fail("Account or repository was not found"), nil
			}
			if status != 204 {
				return Fail(fmt.Sprintf("Failed to delete known host `%s`: HTTP %d", name, status)), nil
			}
		}
		return Changed(""), nil
	}
	return Ok(""), nil
}

// bitbucketHostKey returns the key type and the base64 key to register,
// from the `key` argument when it carries one, and from ssh-keyscan on
// the target otherwise.
func bitbucketHostKey(ctx context.Context, conn remoteexec.Connection, args map[string]any, hostname string) (keyType, key string, res Result, ok bool) {
	if param := argString(args, "key", ""); param != "" {
		t, k, found := strings.Cut(param, " ")
		if !found {
			return "", "", Fail("Public key type is unknown"), false
		}
		return t, k, Result{}, true
	}

	if _, err := run(ctx, conn, "command -v ssh-keyscan"); err != nil {
		return "", "", Fail("bitbucket_pipeline_known_host: the ssh-keyscan binary is required on the target " +
			"when `key` is not given — it is what this port uses in place of real's paramiko handshake"), false
	}
	out, err := run(ctx, conn, "ssh-keyscan -T 10 "+shellQuote(hostname))
	if err != nil {
		return "", "", Fail(fmt.Sprintf("Error opening socket to %s", hostname)), false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// ssh-keyscan writes its progress and errors as `# ...` comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		return fields[1], fields[2], Result{}, true
	}
	return "", "", Fail(fmt.Sprintf("SSH error on retrieving %s server key", hostname)), false
}

// moduleBitbucketPipelineVariable implements Ansible's
// `bitbucket_pipeline_variable` (community.general) module: manages the
// variables a repository's pipelines run with.
//
// Args: repository (required); workspace (required); name (required);
// state (required, present/absent); value; secured (default false); plus
// the four credential arguments.
//
// # A secured variable is ALWAYS reported changed
//
// Bitbucket never returns the value of a secured variable, so real's
// `existing.get('value') != value` can never be false for one, and every
// run updates it and reports changed. That is upstream behaviour, not an
// accident of this port, and it is reproduced rather than corrected: a
// module that quietly reported "ok" where real reports "changed" would
// make a playbook's own `changed_when` behave differently here.
//
// # The pagination is NOT the `next` link
//
// Where deploy keys and known hosts follow the server's `next`, real
// walks pipeline variables by counting `?page=N` until a page comes back
// with `pagelen` equal to zero. The two are reproduced separately
// because they end differently: a `next` walk stops when the server says
// so, and this one stops on an empty page.
func moduleBitbucketPipelineVariable(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	workspace, repository, err := bitbucketRepoArgs(args)
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state, err := bitbucketState(args)
	if err != nil {
		return Result{}, err
	}
	value := argString(args, "value", "")
	secured := argBool(args, "secured", false)
	if value == "" && state == "present" {
		return Fail("`value` is required when the `state` is `present`"), nil
	}

	if res, ok := requireCurl(ctx, conn, "bitbucket_pipeline_variable", "Bitbucket"); !ok {
		return res, nil
	}
	c, res, ok := bitbucketAuth(ctx, conn, args, "bitbucket_pipeline_variable")
	if !ok {
		return res, nil
	}

	listURL := fmt.Sprintf("%s/2.0/repositories/%s/%s/pipelines_config/variables/", bitbucketAPIURL, workspace, repository)
	detailURL := func(uuid string) string {
		return fmt.Sprintf("%s/2.0/repositories/%s/%s/pipelines_config/variables/%s", bitbucketAPIURL, workspace, repository, uuid)
	}

	var existing map[string]any
	for page := 1; ; page++ {
		content, status, err := c.request(ctx, "GET", fmt.Sprintf("%s?page=%d", listURL, page), nil)
		if err != nil {
			return Fail("bitbucket_pipeline_variable: " + err.Error()), nil
		}
		if status == 404 {
			return Fail("Invalid `repository` or `workspace`."), nil
		}
		if status != 200 {
			return Fail(fmt.Sprintf("Failed to retrieve the list of pipeline variables: HTTP %d", status)), nil
		}
		// Real's end-of-list test, and it is `pagelen == 0`, not an
		// empty `values`: a page present but empty ends the walk.
		if pagelen, ok := content["pagelen"].(float64); ok && pagelen == 0 {
			break
		}
		var found bool
		for _, v := range bitbucketValues(content) {
			// The variable's name comes back under `key`; real renames it
			// to `name` on the way out.
			if k, _ := v["key"].(string); k == name {
				existing, found = v, true
				break
			}
		}
		if found {
			break
		}
	}

	switch {
	case existing == nil && state == "present":
		if !InCheckMode(args) {
			_, status, err := c.request(ctx, "POST", listURL, map[string]any{
				"key": name, "value": value, "secured": secured,
			})
			if err != nil {
				return Fail("bitbucket_pipeline_variable: " + err.Error()), nil
			}
			if status != 201 {
				return Fail(fmt.Sprintf("Failed to create pipeline variable `%s`: HTTP %d", name, status)), nil
			}
		}
		return Changed(""), nil

	case existing != nil && state == "present":
		existingSecured, _ := existing["secured"].(bool)
		existingValue, hasValue := existing["value"].(string)
		if existingSecured == secured && hasValue && existingValue == value {
			return Ok(""), nil
		}
		if !InCheckMode(args) {
			uuid, _ := existing["uuid"].(string)
			_, status, err := c.request(ctx, "PUT", detailURL(uuid), map[string]any{
				"value": value, "secured": secured,
			})
			if err != nil {
				return Fail("bitbucket_pipeline_variable: " + err.Error()), nil
			}
			if status != 200 {
				return Fail(fmt.Sprintf("Failed to update pipeline variable `%s`: HTTP %d", name, status)), nil
			}
		}
		return Changed(""), nil

	case existing != nil && state == "absent":
		if !InCheckMode(args) {
			uuid, _ := existing["uuid"].(string)
			_, status, err := c.request(ctx, "DELETE", detailURL(uuid), nil)
			if err != nil {
				return Fail("bitbucket_pipeline_variable: " + err.Error()), nil
			}
			if status != 204 {
				return Fail(fmt.Sprintf("Failed to delete pipeline variable `%s`: HTTP %d", name, status)), nil
			}
		}
		return Changed(""), nil
	}
	return Ok(""), nil
}
