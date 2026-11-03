package modules

import (
	"context"
	"strings"
	"testing"
)

// The fixtures are Online's own documented shapes, taken from the two
// modules' RETURN blocks: /api/v1/server answers with a list of PATHS,
// each path with a server object, and /api/v1/user with one account.

const onlineServerList = `["/api/v1/server/42","/api/v1/server/43"]`

const onlineServer42 = `{"id":42,"hostname":"sd-42","offer":"Pro-1-S-SATA","power":"ON"}`
const onlineServer43 = `{"id":43,"hostname":"sd-43","offer":"Pro-2-M-SSD","power":"OFF"}`

const onlineUser = `{"id":42,"login":"foobar","email":"foobar@example.com","company":"foobar LLC"}`

func ok200(body string) string { return body + "\nHTTPSTATUS:200" }

func TestOnlineUserInfoReturnsTheAccountObject(t *testing.T) {
	const token = "ONLINETOKEN-aaaaaaaaaaaaaaaa"
	conn := &curlConn{bodies: []string{ok200(onlineUser)}}
	res, err := moduleOnlineUserInfo(context.Background(), conn, map[string]any{"api_token": token})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Error("changed = true; real reports changed=False for an info module")
	}
	user, ok := res.Extra["online_user_info"].(map[string]any)
	if !ok {
		t.Fatalf("online_user_info = %#v, want the account object", res.Extra["online_user_info"])
	}
	if user["login"] != "foobar" {
		t.Errorf("login = %v, want the API's own value", user["login"])
	}
	// The endpoint, and the default api_url, are real's.
	if joined := strings.Join(conn.Stdins, "\n"); !strings.Contains(joined, "https://api.online.net/api/v1/user") {
		t.Errorf("request went elsewhere: %q", joined)
	}
	assertSecretsOffArgv(t, conn, map[string]string{"api_token": token})
}

func TestOnlineServerInfoFetchesEachServerPath(t *testing.T) {
	conn := &curlConn{bodies: []string{
		ok200(onlineServerList),
		ok200(onlineServer42),
		ok200(onlineServer43),
	}}
	res, err := moduleOnlineServerInfo(context.Background(), conn, map[string]any{"api_token": "t"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	servers, ok := res.Extra["online_server_info"].([]any)
	if !ok {
		t.Fatalf("online_server_info = %#v, want a list", res.Extra["online_server_info"])
	}
	if len(servers) != 2 {
		t.Fatalf("got %d servers, want the two the list named", len(servers))
	}
	first, _ := servers[0].(map[string]any)
	if first["hostname"] != "sd-42" {
		t.Errorf("first server = %#v, want the DETAIL, not the path", servers[0])
	}
	// One request for the list and one per path: the second GET is the
	// whole difference between this module and online_user_info.
	joined := strings.Join(conn.Stdins, "\n")
	for _, want := range []string{
		"https://api.online.net/api/v1/server\"",
		"https://api.online.net/api/v1/server/42\"",
		"https://api.online.net/api/v1/server/43\"",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("no request for %s:\n%s", want, joined)
		}
	}
}

// TestOnlineServerInfoKeepsAnErrorObjectInTheList pins behaviour this
// port did NOT correct. Real catches OnlineException around the detail
// fetch, but `get` never raises one, so a failed detail lands in the
// list as the API's own error object. A port that quietly dropped it
// would be the kinder module and the wrong one: a playbook counting
// `online_server_info | length` would get a different number here than
// against real.
func TestOnlineServerInfoKeepsAnErrorObjectInTheList(t *testing.T) {
	conn := &curlConn{bodies: []string{
		ok200(onlineServerList),
		ok200(onlineServer42),
		`{"message":"server not found"}` + "\nHTTPSTATUS:404",
	}}
	res, err := moduleOnlineServerInfo(context.Background(), conn, map[string]any{"api_token": "t"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v; real does not fail the module for one bad server", res)
	}
	servers := res.Extra["online_server_info"].([]any)
	if len(servers) != 2 {
		t.Fatalf("got %d entries, want 2 — the server and the error object real leaves in", len(servers))
	}
	second, _ := servers[1].(map[string]any)
	if second["message"] != "server not found" {
		t.Errorf("second entry = %#v, want the error object real keeps", servers[1])
	}
}

// The other half of the same upstream loop: a body that is not JSON is
// dropped, and nothing says which server went missing.
func TestOnlineServerInfoDropsAnUnparseableDetail(t *testing.T) {
	conn := &curlConn{bodies: []string{
		ok200(onlineServerList),
		ok200("<html>gateway timeout</html>"),
		ok200(onlineServer43),
	}}
	res, err := moduleOnlineServerInfo(context.Background(), conn, map[string]any{"api_token": "t"})
	if err != nil {
		t.Fatal(err)
	}
	servers := res.Extra["online_server_info"].([]any)
	if len(servers) != 1 {
		t.Fatalf("got %d servers, want the one that parsed", len(servers))
	}
	if s, _ := servers[0].(map[string]any); s["hostname"] != "sd-43" {
		t.Errorf("wrong survivor: %#v", servers[0])
	}
}

// TestOnlineReadsTheTokenFromTheTargetEnvironment holds both the
// fallback ORDER and the place it looks. The fake answers `printenv`
// only for ONLINE_API_KEY, so a module reading the control node's own
// environment, or reading the names in another order, fails here.
func TestOnlineReadsTheTokenFromTheTargetEnvironment(t *testing.T) {
	const token = "FROMTARGET-bbbbbbbbbbbb"
	conn := &curlConn{
		bodies: []string{ok200(onlineUser)},
		env:    map[string]string{"ONLINE_API_KEY": token},
	}
	res, err := moduleOnlineUserInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(strings.Join(conn.Stdins, "\n"), "Bearer "+token) {
		t.Error("the token from the target's environment never reached the request")
	}
	// ONLINE_TOKEN comes first in real's list and must be asked for
	// first, or an account with both set would authenticate as the
	// wrong one.
	var asked []string
	for _, cmd := range conn.Commands {
		if name, ok := strings.CutPrefix(cmd, "printenv "); ok {
			asked = append(asked, name)
		}
	}
	if len(asked) < 2 || asked[0] != "ONLINE_TOKEN" || asked[1] != "ONLINE_API_KEY" {
		t.Errorf("asked %v, want real's order starting ONLINE_TOKEN, ONLINE_API_KEY", asked)
	}
	assertSecretsOffArgv(t, conn, map[string]string{"api_token": token})
}

func TestOnlineWithoutATokenFails(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(onlineUser)}}
	res, err := moduleOnlineUserInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "missing required arguments: api_token" {
		t.Errorf("res = %+v, want AnsibleModule's own wording", res)
	}
}

// TestOnlineFetchErrorCarriesRealsSentence checks the one message the
// helper builds, word for word, including the parenthesised URL and the
// bracketed status that real interpolates.
func TestOnlineFetchErrorCarriesRealsSentence(t *testing.T) {
	conn := &curlConn{bodies: []string{`{"message":"not authenticated"}` + "\nHTTPSTATUS:403"}}
	res, err := moduleOnlineServerInfo(context.Background(), conn, map[string]any{"api_token": "t"})
	if err != nil {
		t.Fatal(err)
	}
	want := "Error fetching api/v1/server (https://api.online.net/api/v1/server) [403: not authenticated]"
	if !res.Failed || res.Msg != want {
		t.Errorf("msg = %q, want %q", res.Msg, want)
	}
}

// Real subscripts `results.json['message']` and dies with a KeyError or
// a ValueError when the body has neither. This says what happened
// instead; the test exists so the divergence is a decision on record
// rather than an accident.
func TestOnlineFetchErrorWithoutAMessageFallsBackToTheBody(t *testing.T) {
	conn := &curlConn{bodies: []string{"<html>502 Bad Gateway</html>" + "\nHTTPSTATUS:502"}}
	res, err := moduleOnlineUserInfo(context.Background(), conn, map[string]any{"api_token": "t"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "502 Bad Gateway") {
		t.Errorf("msg = %q, want the body where real has a traceback", res.Msg)
	}
}

func TestOnlineHonoursApiUrlAndTimeout(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(onlineUser)}}
	if _, err := moduleOnlineUserInfo(context.Background(), conn, map[string]any{
		"api_token": "t", "base_url": "https://api.staging.example/", "timeout": 7,
	}); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(conn.Stdins, "\n"); !strings.Contains(joined, "https://api.staging.example/api/v1/user") {
		t.Errorf("the base_url alias was ignored: %q", joined)
	}
	// The timeout is a curl flag, not a config directive, so it is the
	// one thing here that belongs on the command line.
	if !strings.Contains(strings.Join(conn.Commands, " "), "--max-time 7") {
		t.Errorf("the timeout alias was ignored: %q", conn.Commands)
	}
}

func TestOnlineValidateCertsFalseReachesCurl(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(onlineUser)}}
	if _, err := moduleOnlineUserInfo(context.Background(), conn, map[string]any{
		"api_token": "t", "validate_certs": false,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(conn.Stdins, "\n"), "insecure") {
		t.Error("validate_certs: false did not turn certificate checking off")
	}
	// Control: the default leaves it on, so the assertion above is
	// about the argument and not about a flag that is always there.
	on := &curlConn{bodies: []string{ok200(onlineUser)}}
	if _, err := moduleOnlineUserInfo(context.Background(), on, map[string]any{"api_token": "t"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(on.Stdins, "\n"), "insecure") {
		t.Error("certificate checking is off by default; real's validate_certs defaults to true")
	}
}

// TestOnlineServerInfoRefusesAHostilePathFromTheAPI is why the curl
// config escape was fixed before these modules were written. The paths
// this module fetches come from the API's own answer, so a compromised
// or impersonated endpoint chooses part of a curl config. It must not
// get to.
func TestOnlineServerInfoRefusesAHostilePathFromTheAPI(t *testing.T) {
	conn := &curlConn{bodies: []string{
		ok200(`["/api/v1/server/42\nurl = file:///etc/passwd\n#"]`),
	}}
	res, err := moduleOnlineServerInfo(context.Background(), conn, map[string]any{"api_token": "t"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("a path carrying a newline was fetched anyway: %+v", res)
	}
	if !strings.Contains(res.Msg, "control character") {
		t.Errorf("msg = %q, want it to name what was wrong", res.Msg)
	}
	// Refused before the request: one curl invocation, the list, and no
	// second one.
	var curls int
	for _, cmd := range conn.Commands {
		if strings.HasPrefix(cmd, "curl ") {
			curls++
		}
	}
	if curls != 1 {
		t.Errorf("curl ran %d times, want only the list request", curls)
	}
}

func TestOnlineWithoutCurlSaysSo(t *testing.T) {
	conn := &curlConn{noCurl: true}
	res, err := moduleOnlineUserInfo(context.Background(), conn, map[string]any{"api_token": "t"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "curl binary is required") {
		t.Errorf("res = %+v, want the missing-curl explanation", res)
	}
}
