package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// Online.net's two info modules sit on one private helper upstream,
// `module_utils/_online.py`, exactly as Bitbucket's four sit on
// `_bitbucket.py`. That shape is kept here: one client reproducing the
// helper, two modules over it.
//
// Online has no CLI of any kind -- only the REST API documented at
// console.online.net/en/api -- so every request goes through curl ON
// THE TARGET, with the URL, the bearer token and the headers on curl's
// stdin config. The api_token is a credential and never reaches argv;
// TestOnlineKeepsTheTokenOffArgv holds that and carries its own control.
//
// # What `get_resources` returns is not the same shape twice
//
// The helper's `get_resources` is one method, but the two modules use
// it for two different things. `online_user_info` returns its body
// straight through -- a single user object. `online_server_info` gets
// back a LIST OF PATHS, `["/api/v1/server/42", ...]`, and then fetches
// each one. The modules read alike upstream and do not behave alike,
// which is why the list walk lives in the server module and not in the
// client.

// onlineAPIURL is real's own default for api_url.
const onlineAPIURL = "https://api.online.net"

// onlineUserAgent is a stated divergence. Real sends
// `ansible <version> Python <version>`, built from the module's own
// interpreter. Neither half of that is true here, and sending it would
// put a false line in Online's own request logs -- the operator reading
// them would be told a Python module made a call that a Go binary made.
// So the port names itself.
const onlineUserAgent = "go-ansible (github.com/go-ansible/modules)"

// onlineClient is `_online.py`'s Online class: the credentials and
// endpoint for one module run, plus the one GET both modules use.
type onlineClient struct {
	conn remoteexec.Connection

	token    string
	apiURL   string
	timeout  int
	insecure bool
}

// onlineAuth reproduces `online_argument_spec()`.
//
// The four token fallbacks are read in real's own order, and ON THE
// TARGET: real evaluates env_fallback inside the module process, which
// runs on the target, so that is where ONLINE_TOKEN has to be looked
// for. Reading the control node's environment would quietly send the
// operator's own token to a host that was meant to supply its own.
//
// One divergence worth stating: real's env_fallback takes a variable
// that is SET BUT EMPTY and returns "", where this cannot tell that
// apart from unset and moves on to the next name. For api_token the two
// paths end in the same place by different routes -- real would send
// `Bearer ` and be refused by Online with a 401, this one fails on the
// missing argument before any request goes out.
func onlineAuth(ctx context.Context, conn remoteexec.Connection, args map[string]any, module string) (*onlineClient, Result, bool) {
	token := argStringAliased(args, "api_token", "oauth_token", "")
	if token == "" {
		for _, name := range []string{"ONLINE_TOKEN", "ONLINE_API_KEY", "ONLINE_OAUTH_TOKEN", "ONLINE_API_TOKEN"} {
			v, err := targetEnv(ctx, conn, name)
			if err != nil {
				return nil, Fail(module + ": " + err.Error()), false
			}
			if v != "" {
				token = v
				break
			}
		}
	}
	if token == "" {
		// AnsibleModule's own wording for a required argument that no
		// fallback supplied.
		return nil, Fail("missing required arguments: api_token"), false
	}

	apiURL := argStringAliased(args, "api_url", "base_url", "")
	if apiURL == "" {
		v, err := targetEnv(ctx, conn, "ONLINE_API_URL")
		if err != nil {
			return nil, Fail(module + ": " + err.Error()), false
		}
		apiURL = v
	}
	if apiURL == "" {
		apiURL = onlineAPIURL
	}

	return &onlineClient{
		conn:    conn,
		token:   token,
		apiURL:  strings.TrimSuffix(apiURL, "/"),
		timeout: argInt(args, "api_timeout", argInt(args, "timeout", 30)),
		// validate_certs defaults to true in real, so the insecure flag
		// is the negation and defaults to off.
		insecure: !argBool(args, "validate_certs", true),
	}, Result{}, true
}

// urlFor is `_url_builder`: one leading slash is dropped, and the rest
// is joined to api_url.
//
// Real indexes `path[0]` and raises IndexError on an empty path. No
// caller passes one -- both modules build the path from a constant or
// from the API's own answer -- so this returns api_url with a trailing
// slash rather than reproducing a crash nothing can reach.
func (c *onlineClient) urlFor(path string) string {
	return c.apiURL + "/" + strings.TrimPrefix(path, "/")
}

// get issues the helper's GET with the helper's three headers.
func (c *onlineClient) get(ctx context.Context, path string) (string, int, error) {
	return curlDo(ctx, c.conn, curlReq{
		Method: "GET",
		URL:    c.urlFor(path),
		Headers: [][2]string{
			{"Authorization", "Bearer " + c.token},
			{"User-Agent", onlineUserAgent},
			// Real spells it "Content-type", lowercase t, on a GET with
			// no body. Headers are case-insensitive and Online does not
			// care, but the request this port sends is the request real
			// sends.
			{"Content-type", "application/json"},
		},
		Timeout:  c.timeout,
		Insecure: c.insecure,
	})
}

// onlineOK is Response.ok: the four statuses the helper treats as
// success.
func onlineOK(status int) bool {
	switch status {
	case 200, 201, 202, 204:
		return true
	}
	return false
}

// getResources is the helper's own method, including its error
// sentence. `name` is the resource path without its leading slash, the
// way both modules set `self.name`.
func (c *onlineClient) getResources(ctx context.Context, name string) (string, error) {
	body, status, err := c.get(ctx, "/"+name)
	if err != nil {
		return "", err
	}
	if !onlineOK(status) {
		return "", fmt.Errorf("Error fetching %s (%s/%s) [%d: %s]",
			name, c.apiURL, name, status, onlineErrorMessage(body))
	}
	return body, nil
}

// onlineErrorMessage is `results.json['message']`, with the part real
// does not have.
//
// Real subscripts the decoded body directly, so an error whose body is
// not JSON, or is JSON without a `message`, makes the module die with a
// ValueError or a KeyError and no message at all -- a traceback instead
// of the sentence the code was trying to build. That is a defect, not a
// contract, and reproducing it would mean losing the one thing the
// operator needs. The body itself is used in its place, trimmed.
func onlineErrorMessage(body string) string {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err == nil {
		if msg, ok := decoded["message"].(string); ok {
			return msg
		}
	}
	return strings.TrimSpace(body)
}

// moduleOnlineUserInfo implements Ansible's `online_user_info`
// (community.general) module: gathers information about the account the
// api_token belongs to.
//
// Args: api_token (required, alias oauth_token, falling back to
// ONLINE_TOKEN, ONLINE_API_KEY, ONLINE_OAUTH_TOKEN or ONLINE_API_TOKEN
// on the target); api_url (alias base_url, falling back to
// ONLINE_API_URL, default https://api.online.net); api_timeout (alias
// timeout, default 30); validate_certs (default true).
//
// Returns online_user_info: the API's own object for the account,
// passed through unchanged, which is what real returns.
//
// It is one GET of /api/v1/user and nothing else -- the simplest shape
// in the family, and the one that shows what `get_resources` does when
// the resource is a single object rather than a collection.
func moduleOnlineUserInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return onlineInfo(ctx, conn, args, "online_user_info", "api/v1/user",
		func(ctx context.Context, c *onlineClient, body string) (any, Result, bool) {
			var user any
			if err := json.Unmarshal([]byte(body), &user); err != nil {
				return nil, Fail("online_user_info: the API's answer is not JSON: " + strings.TrimSpace(body)), false
			}
			return user, Result{}, true
		})
}

// moduleOnlineServerInfo implements Ansible's `online_server_info`
// (community.general) module: gathers information about every dedicated
// server on the account.
//
// Args: the same four as online_user_info.
//
// Returns online_server_info: a list of the API's own server objects.
//
// # Two requests deep, and the second one's failures are silent
//
// GET /api/v1/server returns a list of PATHS, not of servers, so real
// fetches each path and collects what comes back. Two upstream
// behaviours in that loop are reproduced rather than corrected:
//
//   - A server whose detail fetch fails does NOT fail the module. Real
//     wraps the fetch in `except OnlineException`, but `get` never
//     raises one -- it returns the response -- so a 404 or a 500 lands
//     in the list as the API's own ERROR OBJECT, alongside the real
//     servers. A caller iterating the result gets `{"message": ...}`
//     where it expected a server.
//   - A detail body that is not JSON is dropped silently. Real's
//     `Response.json` returns None for one and the list comprehension
//     filters None out, so the server simply vanishes from the
//     inventory with nothing said.
//
// The second is where this port stops short of real: when the body is
// unparseable AND the status is an error, real reaches the branch that
// does not catch ValueError and dies with a traceback. This drops it
// the way it drops every other unparseable body. Both lose the server;
// only one of them tells you which process died.
func moduleOnlineServerInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return onlineInfo(ctx, conn, args, "online_server_info", "api/v1/server",
		func(ctx context.Context, c *onlineClient, body string) (any, Result, bool) {
			var paths []string
			if err := json.Unmarshal([]byte(body), &paths); err != nil {
				return nil, Fail("online_server_info: /api/v1/server did not answer with a list of server paths: " +
					strings.TrimSpace(body)), false
			}
			// Real builds a list and returns it; an account with no
			// servers gets an empty one, not a null.
			servers := make([]any, 0, len(paths))
			for _, path := range paths {
				detail, _, err := c.get(ctx, path)
				if err != nil {
					// Not a status this time: curl itself could not run,
					// or the path carried something that cannot be in a
					// URL. Real has no equivalent because it never shells
					// out, and swallowing it would turn a broken target
					// into an empty inventory.
					return nil, Fail(fmt.Sprintf("online_server_info: fetching %s: %v", path, err)), false
				}
				var server any
				if err := json.Unmarshal([]byte(detail), &server); err != nil {
					continue
				}
				if server == nil {
					continue
				}
				servers = append(servers, server)
			}
			return servers, Result{}, true
		})
}

// onlineInfo is the shape both modules share: authenticate, fetch the
// resource, hand the body to the module's own reader, and return it
// under the module's own key.
//
// Both declare supports_check_mode=True upstream and both only ever
// GET, so there is no check-mode branch to write: the run is the same
// either way, which is exactly why they are in checkModeSupport.
func onlineInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any, module, resource string,
	read func(context.Context, *onlineClient, string) (any, Result, bool),
) (Result, error) {
	if res, ok := requireCurl(ctx, conn, module, "Online.net"); !ok {
		return res, nil
	}
	c, res, ok := onlineAuth(ctx, conn, args, module)
	if !ok {
		return res, nil
	}
	body, err := c.getResources(ctx, resource)
	if err != nil {
		return Fail(err.Error()), nil
	}
	value, res, ok := read(ctx, c, body)
	if !ok {
		return res, nil
	}
	// Real returns changed=False and no message; the information is the
	// whole result.
	return Ok("").WithExtra(module, value), nil
}
