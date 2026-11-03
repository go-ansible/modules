package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// bbHTTP wraps a JSON body in the trailer curlDo's own parser reads, so
// a test says what the server answered rather than how curl reports it.
func bbHTTP(status int, body string) string {
	return body + "\nHTTPSTATUS:" + strconv.Itoa(status)
}

const (
	bbClientID = "BBCLIENT-0123456789abcdef"
	bbSecret   = "BBSECRET-fedcba9876543210"
	bbToken    = "BBTOKEN-aaaabbbbccccdddd"
)

// tokenGrant is the first answer every client-credentials run needs.
func tokenGrant() string {
	return bbHTTP(200, `{"access_token": "`+bbToken+`", "token_type": "bearer"}`)
}

func bbCreds() map[string]any {
	return map[string]any{
		"client_id":     bbClientID,
		"client_secret": bbSecret,
		"repository":    "test",
		"workspace":     "mleu",
	}
}

// bodiesOf returns the JSON bodies the module sent, in order, decoded.
// A request with no body contributes nothing, so the indices here are
// WRITES rather than requests.
func bodiesOf(t *testing.T, conn *curlConn) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, s := range conn.Stdins {
		for _, line := range strings.Split(s, "\n") {
			raw, ok := strings.CutPrefix(line, "data = \"")
			if !ok {
				continue
			}
			raw = curlConfigUnescape(strings.TrimSuffix(raw, "\""))
			var m map[string]any
			if err := json.Unmarshal([]byte(raw), &m); err != nil {
				continue // a form body, not JSON
			}
			out = append(out, m)
		}
	}
	return out
}

func TestBitbucketAccessKeyCreatesWhenAbsent(t *testing.T) {
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(200, `{"values": [{"id": 1, "label": "other", "key": "ssh-rsa OTHER"}]}`),
		bbHTTP(200, `{}`),
	}}
	args := bbCreds()
	args["label"] = "mykey"
	args["key"] = "ssh-rsa AAAAnew"
	args["state"] = "present"

	res, err := moduleBitbucketAccessKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v, want changed", res)
	}
	bodies := bodiesOf(t, conn)
	if len(bodies) != 1 {
		t.Fatalf("sent %d JSON bodies, want 1 (the create): %v", len(bodies), bodies)
	}
	if bodies[0]["label"] != "mykey" || bodies[0]["key"] != "ssh-rsa AAAAnew" {
		t.Errorf("create body = %v", bodies[0])
	}
}

// TestBitbucketAccessKeyComparesByPrefix pins the one comparison that
// looks like a bug and is not. Bitbucket stores a deploy key WITHOUT its
// trailing comment, so real asks whether the new key STARTS WITH the
// stored one rather than whether the two are equal. An equality test
// would rewrite the key on every run for anyone whose key carries a
// comment, which is most people.
func TestBitbucketAccessKeyComparesByPrefix(t *testing.T) {
	const stored = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQAB"

	for _, tc := range []struct {
		name    string
		key     string
		changed bool
	}{
		{"the same key with a comment appended is NOT a change", stored + " user@host", false},
		{"the same key exactly is not a change", stored, false},
		{"a different key is a change", "ssh-rsa AAAAC3NzaC1lZDI1NTE5", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &curlConn{bodies: []string{
				tokenGrant(),
				bbHTTP(200, `{"values": [{"id": 7, "label": "mykey", "key": "`+stored+`"}]}`),
				bbHTTP(204, ``), // delete, if it comes to that
				bbHTTP(200, `{}`),
			}}
			args := bbCreds()
			args["label"] = "mykey"
			args["key"] = tc.key
			args["state"] = "present"

			res, err := moduleBitbucketAccessKey(context.Background(), conn, args)
			if err != nil {
				t.Fatal(err)
			}
			if res.Failed {
				t.Fatalf("res = %+v", res)
			}
			if res.Changed != tc.changed {
				t.Errorf("changed = %v, want %v", res.Changed, tc.changed)
			}
			// The shape of the update is the other half: Bitbucket has no
			// update under the same label, so a change must be a DELETE
			// followed by a POST, not a PUT.
			if tc.changed {
				var methods []string
				for _, s := range conn.Stdins {
					for _, line := range strings.Split(s, "\n") {
						if m, ok := strings.CutPrefix(line, "request = \""); ok {
							methods = append(methods, strings.TrimSuffix(m, "\""))
						}
					}
				}
				joined := strings.Join(methods, ",")
				if !strings.HasSuffix(joined, "DELETE,POST") {
					t.Errorf("methods = %s; an update must be DELETE then POST", joined)
				}
			}
		})
	}
}

// TestBitbucketAccessKeyFollowsPagination holds the walk that would
// otherwise pass by accident: the key lives on the SECOND page, so a
// module that read one page and stopped would report it absent and
// create a duplicate.
func TestBitbucketAccessKeyFollowsPagination(t *testing.T) {
	const next = "https://api.bitbucket.org/2.0/repositories/mleu/test/deploy-keys/?page=2"
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(200, `{"values": [{"id": 1, "label": "other", "key": "k1"}], "next": "`+next+`"}`),
		bbHTTP(200, `{"values": [{"id": 2, "label": "mykey", "key": "ssh-rsa STORED"}]}`),
	}}
	args := bbCreds()
	args["label"] = "mykey"
	args["key"] = "ssh-rsa STORED with a comment"
	args["state"] = "present"

	res, err := moduleBitbucketAccessKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v; the key is on page 2 and matches, so nothing should change", res)
	}
	// The control: the second page must actually have been asked for.
	var asked bool
	for _, s := range conn.Stdins {
		if strings.Contains(s, "page=2") {
			asked = true
		}
	}
	if !asked {
		t.Error("the `next` link was never followed; this test would pass on a one-page walk too")
	}
}

func TestBitbucketPipelineVariableWalksByPageNumber(t *testing.T) {
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(200, `{"pagelen": 1, "values": [{"uuid": "{u1}", "key": "OTHER", "value": "x", "secured": false}]}`),
		bbHTTP(200, `{"pagelen": 1, "values": [{"uuid": "{u2}", "key": "AWS_KEY", "value": "old", "secured": false}]}`),
		bbHTTP(200, `{}`),
	}}
	args := bbCreds()
	args["name"] = "AWS_KEY"
	args["value"] = "new"
	args["state"] = "present"

	res, err := moduleBitbucketPipelineVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v, want changed", res)
	}
	// Real walks this collection by COUNTING pages, not by following a
	// `next` link, and the fixtures above carry no `next` at all: a
	// module that followed links would have stopped on page 1 and
	// created a duplicate.
	var sawPage2 bool
	for _, s := range conn.Stdins {
		if strings.Contains(s, "page=2") {
			sawPage2 = true
		}
	}
	if !sawPage2 {
		t.Error("page 2 was never requested; pipeline variables are walked by page number")
	}
	bodies := bodiesOf(t, conn)
	if len(bodies) != 1 || bodies[0]["value"] != "new" {
		t.Errorf("update body = %v", bodies)
	}
}

// TestBitbucketPipelineVariableEndsOnPagelenZero pins the terminator.
// `values` empty is NOT the end for real -- `pagelen == 0` is -- and a
// walk that stopped on an empty `values` would never reach a variable
// sitting after a sparse page.
func TestBitbucketPipelineVariableEndsOnPagelenZero(t *testing.T) {
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(200, `{"pagelen": 10, "values": []}`),
		bbHTTP(200, `{"pagelen": 1, "values": [{"uuid": "{u}", "key": "FOUND", "value": "v", "secured": false}]}`),
	}}
	args := bbCreds()
	args["name"] = "FOUND"
	args["value"] = "v"
	args["state"] = "present"

	res, err := moduleBitbucketPipelineVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Errorf("changed = true; the variable was found on page 2 with the same value")
	}
}

// TestBitbucketPipelineVariableSecuredIsAlwaysChanged reproduces an
// upstream behaviour rather than correcting it: Bitbucket never returns
// a secured variable's value, so real's `existing.get('value') != value`
// can never be false, and every run updates and reports changed.
// Reporting "ok" here would make a playbook's `changed_when` behave
// differently under this port than under real.
func TestBitbucketPipelineVariableSecuredIsAlwaysChanged(t *testing.T) {
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(200, `{"pagelen": 1, "values": [{"uuid": "{u}", "key": "SECRET", "secured": true}]}`),
		bbHTTP(200, `{}`),
	}}
	args := bbCreds()
	args["name"] = "SECRET"
	args["value"] = "unchanged"
	args["secured"] = true
	args["state"] = "present"

	res, err := moduleBitbucketPipelineVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("changed = false; a secured variable has no value to compare and real always updates it")
	}
}

func TestBitbucketPipelineKeyPairComparesOnlyThePublicKey(t *testing.T) {
	const pub = "ssh-rsa AAAAPUB"
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(200, `{"public_key": "`+pub+`", "type": "pipeline_ssh_key_pair"}`),
	}}
	args := bbCreds()
	args["public_key"] = pub
	args["private_key"] = "-----BEGIN OPENSSH PRIVATE KEY-----\nrotated\n"
	args["state"] = "present"

	res, err := moduleBitbucketPipelineKeyPair(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	// Bitbucket never returns the private key, so a rotated private key
	// alone is invisible — in real exactly as here. Asserting it is how
	// the limitation stays documented in something that runs.
	if res.Changed {
		t.Error("changed = true; only the public key is comparable, and it matched")
	}
}

func TestBitbucketPipelineKeyPair404MeansAbsentNotBroken(t *testing.T) {
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(404, `{"error": {"message": "Not found"}}`),
		bbHTTP(200, `{}`),
	}}
	args := bbCreds()
	args["public_key"] = "ssh-rsa AAAAPUB"
	args["private_key"] = "private"
	args["state"] = "present"

	res, err := moduleBitbucketPipelineKeyPair(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v; a 404 on the GET means there is no key pair yet, not an error", res)
	}
}

func TestBitbucketKnownHostSplitsAnExplicitKey(t *testing.T) {
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(200, `{"values": []}`),
		bbHTTP(201, `{}`),
	}}
	args := bbCreds()
	args["name"] = "bitbucket.org"
	args["key"] = "ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAubiN81eDcafrg"
	args["state"] = "present"

	res, err := moduleBitbucketPipelineKnownHost(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	bodies := bodiesOf(t, conn)
	if len(bodies) != 1 {
		t.Fatalf("sent %d bodies, want 1: %v", len(bodies), bodies)
	}
	pk, _ := bodies[0]["public_key"].(map[string]any)
	if pk["key_type"] != "ssh-rsa" || pk["key"] != "AAAAB3NzaC1yc2EAAAABIwAAAQEAubiN81eDcafrg" {
		t.Errorf("public_key = %v; the key must be split on the FIRST space", pk)
	}
	// ssh-keyscan must not have been reached: a key was given.
	for _, cmd := range conn.Commands {
		if strings.Contains(cmd, "ssh-keyscan") {
			t.Errorf("ssh-keyscan ran although `key` was given: %q", cmd)
		}
	}
}

func TestBitbucketKnownHostRefusesAKeyWithoutAType(t *testing.T) {
	conn := &curlConn{bodies: []string{tokenGrant(), bbHTTP(200, `{"values": []}`)}}
	args := bbCreds()
	args["name"] = "bitbucket.org"
	args["key"] = "AAAAB3NzaC1yc2EAAAABIwAAAQEAubiN81eDcafrg"
	args["state"] = "present"

	res, err := moduleBitbucketPipelineKnownHost(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "Public key type is unknown" {
		t.Errorf("res = %+v, want real's refusal", res)
	}
}

func TestBitbucketKnownHostScansTheTargetWhenNoKeyGiven(t *testing.T) {
	conn := &curlConn{
		bodies: []string{
			tokenGrant(),
			bbHTTP(200, `{"values": []}`),
			bbHTTP(201, `{}`),
		},
		keyscan: "# bitbucket.org:22 SSH-2.0-OpenSSH\n" +
			"bitbucket.org ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAzS\n" +
			"bitbucket.org ssh-rsa AAAAB3NzaC1yc2EAAAAB\n",
	}
	args := bbCreds()
	args["name"] = "bitbucket.org"
	args["state"] = "present"

	res, err := moduleBitbucketPipelineKnownHost(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	bodies := bodiesOf(t, conn)
	pk, _ := bodies[0]["public_key"].(map[string]any)
	// The FIRST key line, and the `#` comment lines ssh-keyscan writes
	// must not be mistaken for one.
	if pk["key_type"] != "ssh-ed25519" || pk["key"] != "AAAAC3NzaC1lZDI1NTE5AAAAIAzS" {
		t.Errorf("public_key = %v", pk)
	}
}

// TestBitbucketCheckModeLooksButDoesNotWrite is the dry-run contract for
// all four. Real declares supports_check_mode=True on every one and
// guards only the writes, so the lookup still happens and the verdict is
// real.
func TestBitbucketCheckModeLooksButDoesNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fn     func(context.Context, remoteexec.Connection, map[string]any) (Result, error)
		bodies []string
		args   map[string]any
	}{
		{
			name:   "bitbucket_access_key",
			fn:     moduleBitbucketAccessKey,
			bodies: []string{tokenGrant(), bbHTTP(200, `{"values": []}`)},
			args:   map[string]any{"label": "mykey", "key": "ssh-rsa NEW", "state": "present"},
		},
		{
			name:   "bitbucket_pipeline_key_pair",
			fn:     moduleBitbucketPipelineKeyPair,
			bodies: []string{tokenGrant(), bbHTTP(404, ``)},
			args:   map[string]any{"public_key": "pub", "private_key": "priv", "state": "present"},
		},
		{
			name:   "bitbucket_pipeline_known_host",
			fn:     moduleBitbucketPipelineKnownHost,
			bodies: []string{tokenGrant(), bbHTTP(200, `{"values": []}`)},
			args:   map[string]any{"name": "bitbucket.org", "key": "ssh-rsa AAAA", "state": "present"},
		},
		{
			name:   "bitbucket_pipeline_variable",
			fn:     moduleBitbucketPipelineVariable,
			bodies: []string{tokenGrant(), bbHTTP(200, `{"pagelen": 0, "values": []}`)},
			args:   map[string]any{"name": "V", "value": "x", "state": "present"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !SupportsCheckMode(tc.name) {
				t.Fatalf("%s is not registered as supporting check mode", tc.name)
			}
			conn := &curlConn{bodies: tc.bodies}
			args := bbCreds()
			for k, v := range tc.args {
				args[k] = v
			}
			args["_ansible_check_mode"] = true

			res, err := tc.fn(context.Background(), conn, args)
			if err != nil {
				t.Fatal(err)
			}
			if res.Failed || !res.Changed {
				t.Fatalf("res = %+v; a dry run still reports what WOULD change", res)
			}
			// The verdict: no write was sent. Every scripted answer above
			// stops at the lookup, so a module that wrote would run past
			// the queue and fail loudly rather than quietly pass.
			for _, s := range conn.Stdins {
				for _, line := range strings.Split(s, "\n") {
					m, ok := strings.CutPrefix(line, "request = \"")
					if !ok {
						continue
					}
					switch strings.TrimSuffix(m, "\"") {
					case "PUT", "DELETE":
						t.Errorf("a dry run sent %s", m)
					case "POST":
						// The token grant is a POST and is not a write.
						if !strings.Contains(s, "grant_type") {
							t.Errorf("a dry run POSTed to the API: %q", s)
						}
					}
				}
			}
			for _, cmd := range conn.Commands {
				if strings.Contains(cmd, "ssh-keyscan") {
					t.Errorf("a dry run scanned a remote host's SSH key: %q", cmd)
				}
			}
		})
	}
}

// TestBitbucketKeepsSecretsOffArgv is why every request goes through
// curl's stdin config. Four different credentials pass through one run:
// the OAuth client id and secret on the token exchange, the bearer token
// on every call after it, and a pipeline variable's own value, which is
// a secret by construction.
func TestBitbucketKeepsSecretsOffArgv(t *testing.T) {
	const value = "VARVALUE-secret-0123456789"
	conn := &curlConn{bodies: []string{
		tokenGrant(),
		bbHTTP(200, `{"pagelen": 0, "values": []}`),
		bbHTTP(201, `{}`),
	}}
	args := bbCreds()
	args["name"] = "AWS_SECRET_ACCESS_KEY"
	args["value"] = value
	args["secured"] = true
	args["state"] = "present"

	if _, err := moduleBitbucketPipelineVariable(context.Background(), conn, args); err != nil {
		t.Fatal(err)
	}
	assertSecretsOffArgv(t, conn, map[string]string{
		"client_id":     bbClientID,
		"client_secret": bbSecret,
		"access_token":  bbToken,
		"value":         value,
	})
}

// TestBitbucketBasicAuthIsUsedWhenThereIsNoClientID pins the helper's
// order: a bearer token wins when there is one, and user/password is
// what is left. The control is that NO token exchange happened — a run
// that fetched one anyway would have consumed a scripted answer and
// shifted every assertion below.
func TestBitbucketBasicAuthIsUsedWhenThereIsNoClientID(t *testing.T) {
	const password = "BBPASSWORD-9876543210"
	conn := &curlConn{bodies: []string{
		bbHTTP(200, `{"values": []}`),
		bbHTTP(200, `{}`),
	}}
	args := map[string]any{
		"repository": "test", "workspace": "mleu",
		"user": "alice", "password": password,
		"label": "mykey", "key": "ssh-rsa NEW", "state": "present",
	}

	res, err := moduleBitbucketAccessKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if strings.Contains(joined, "grant_type") {
		t.Error("a token exchange ran although no client_id was given")
	}
	if !strings.Contains(joined, `user = "alice:`+password+`"`) {
		t.Error("basic auth never reached curl's stdin config")
	}
	assertSecretsOffArgv(t, conn, map[string]string{"password": password})
}

func TestBitbucketRequiresCredentials(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "nothing at all",
			args: map[string]any{},
			want: "one of the following is required: client_id, client_secret, user, password",
		},
		{
			name: "a client id without its secret",
			args: map[string]any{"client_id": bbClientID},
			want: "parameters are required together: client_id, client_secret",
		},
		{
			name: "a user without a password",
			args: map[string]any{"user": "alice"},
			want: "parameters are required together: user, password",
		},
		{
			name: "a username, the alias, without a password",
			args: map[string]any{"username": "alice"},
			want: "parameters are required together: user, password",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &curlConn{bodies: []string{bbHTTP(200, `{"values": []}`)}}
			args := map[string]any{
				"repository": "test", "workspace": "mleu",
				"label": "mykey", "key": "ssh-rsa NEW", "state": "present",
			}
			for k, v := range tc.args {
				args[k] = v
			}
			res, err := moduleBitbucketAccessKey(context.Background(), conn, args)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Failed || res.Msg != tc.want {
				t.Errorf("res = %+v, want %q", res, tc.want)
			}
		})
	}
}

// TestBitbucketReadsCredentialsFromTheTargetEnvironment holds where the
// fallback looks. Real evaluates env_fallback inside the module process,
// which runs on the TARGET; reading the control node's environment here
// would send the operator's own token to a host that was meant to supply
// its own.
func TestBitbucketReadsCredentialsFromTheTargetEnvironment(t *testing.T) {
	conn := &curlConn{
		bodies: []string{tokenGrant(), bbHTTP(200, `{"values": []}`), bbHTTP(200, `{}`)},
		env: map[string]string{
			"BITBUCKET_CLIENT_ID":     bbClientID,
			"BITBUCKET_CLIENT_SECRET": bbSecret,
		},
	}
	args := map[string]any{
		"repository": "test", "workspace": "mleu",
		"label": "mykey", "key": "ssh-rsa NEW", "state": "present",
	}

	res, err := moduleBitbucketAccessKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v; the credentials were in the target's environment", res)
	}
	// The control: the variables must have been asked of the TARGET.
	var asked []string
	for _, cmd := range conn.Commands {
		if name, ok := strings.CutPrefix(cmd, "printenv "); ok {
			asked = append(asked, name)
		}
	}
	if len(asked) == 0 {
		t.Fatal("no environment variable was read from the target at all")
	}
	assertSecretsOffArgv(t, conn, map[string]string{
		"client_id": bbClientID, "client_secret": bbSecret, "access_token": bbToken,
	})
}

func TestTargetEnvUnsetIsEmptyNotAnError(t *testing.T) {
	conn := &curlConn{env: map[string]string{"SET_ONE": "value"}}

	got, err := targetEnv(context.Background(), conn, "SET_ONE")
	if err != nil || got != "value" {
		t.Fatalf("targetEnv(SET_ONE) = %q, %v", got, err)
	}
	// The control for the case that matters: an UNSET variable makes
	// printenv exit 1, and reading that as a failure would make every
	// module with an env fallback refuse to run without one.
	got, err = targetEnv(context.Background(), conn, "NOT_SET")
	if err != nil {
		t.Errorf("an unset variable returned an error: %v", err)
	}
	if got != "" {
		t.Errorf("targetEnv(NOT_SET) = %q, want \"\"", got)
	}
	if _, err := targetEnv(context.Background(), conn, "not a name"); err == nil {
		t.Error("a name that is not an environment variable name was accepted")
	}
}
