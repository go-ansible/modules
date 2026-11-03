package modules

import (
	"context"
	"strings"
	"testing"
)

func TestIpifyFactsSetsThePublicIPFact(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(`{"ip":"203.0.113.9"}`)}}
	res, err := moduleIpifyFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Error("changed = true; real sets changed=False explicitly")
	}
	// A FACT, not an extra field: a later task reads it by name.
	if got := res.Facts["ipify_public_ip"]; got != "203.0.113.9" {
		t.Errorf("ipify_public_ip = %#v, want the address the service returned", got)
	}
	if len(res.Extra) != 0 {
		t.Errorf("extra = %#v; real returns the address under ansible_facts only", res.Extra)
	}
	if joined := strings.Join(conn.Stdins, "\n"); !strings.Contains(joined, "https://api.ipify.org/?format=json") {
		t.Errorf("request was not real's default URL: %q", joined)
	}
}

// TestIpifyFactsConcatenatesTheFormatQuery pins behaviour this port did
// NOT repair. Real builds `f"{api_url}?format=json"`, so an api_url that
// already carries a query produces a URL with two `?` in it. Merging the
// two would make this port answer where real fails.
func TestIpifyFactsConcatenatesTheFormatQuery(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(`{"ip":"198.51.100.4"}`)}}
	if _, err := moduleIpifyFacts(context.Background(), conn, map[string]any{
		"api_url": "https://ipify.example/api?tenant=42",
	}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, "https://ipify.example/api?tenant=42?format=json") {
		t.Errorf("the URL was repaired rather than reproduced: %q", joined)
	}
}

func TestIpifyFactsHonoursTimeoutAndValidateCerts(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(`{"ip":"203.0.113.9"}`)}}
	if _, err := moduleIpifyFacts(context.Background(), conn, map[string]any{
		"timeout": 20, "validate_certs": false,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(conn.Commands, " "), "--max-time 20") {
		t.Errorf("timeout ignored: %q", conn.Commands)
	}
	if !strings.Contains(strings.Join(conn.Stdins, "\n"), "insecure") {
		t.Error("validate_certs: false did not turn certificate checking off")
	}

	// Control: the defaults leave both alone, so the assertions above
	// are about the arguments and not about flags that are always there.
	def := &curlConn{bodies: []string{ok200(`{"ip":"203.0.113.9"}`)}}
	if _, err := moduleIpifyFacts(context.Background(), def, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(def.Commands, " "), "--max-time 10") {
		t.Errorf("default timeout is not real's 10: %q", def.Commands)
	}
	if strings.Contains(strings.Join(def.Stdins, "\n"), "insecure") {
		t.Error("certificate checking is off by default; real's validate_certs defaults to true")
	}
}

// Real's "(timeout)" message is reached only when fetch_url returns no
// response object at all -- a DNS failure, a refused connection, a real
// timeout. Here that is curl failing to run the request.
func TestIpifyFactsNoResponseCarriesRealsSentence(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(`{}`)}, rc: 7}
	res, err := moduleIpifyFacts(context.Background(), conn, map[string]any{"timeout": 3})
	if err != nil {
		t.Fatal(err)
	}
	want := "No valid or no response from url https://api.ipify.org/ within 3 seconds (timeout)"
	if !res.Failed || res.Msg != want {
		t.Errorf("msg = %q, want real's wording %q", res.Msg, want)
	}
	// Real's message names api_url WITHOUT the ?format=json it appended.
	if strings.Contains(res.Msg, "format=json") {
		t.Error("the message names the built URL; real names api_url")
	}
}

// An HTTP error status is where this port stops short of real. Real's
// own guard does not fire -- fetch_url sets `r = e` on an HTTPError,
// after consuming the body -- so it reaches json.loads("") and raises a
// ValueError. This says which status came back instead.
func TestIpifyFactsHTTPErrorNamesTheStatus(t *testing.T) {
	conn := &curlConn{bodies: []string{"" + "\nHTTPSTATUS:503"}}
	res, err := moduleIpifyFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "503") {
		t.Errorf("res = %+v, want the status named where real raises", res)
	}
	if strings.Contains(res.Msg, "(timeout)") {
		t.Error("a 503 reported as a timeout; that is real's confusion, not a contract to copy")
	}
}

// `data.get("ip")` on a body with no `ip` is None, and real sets the
// fact to it rather than leaving it out. A task doing
// `when: ipify_public_ip is defined` must see the same thing here.
func TestIpifyFactsAnswerWithoutAnIPSetsTheFactToNull(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(`{"error":"quota"}`)}}
	res, err := moduleIpifyFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v; real does not fail on a body without an ip", res)
	}
	v, ok := res.Facts["ipify_public_ip"]
	if !ok {
		t.Fatal("the fact is absent; real sets it to None")
	}
	if v != nil {
		t.Errorf("ipify_public_ip = %#v, want nil", v)
	}
}

func TestIpifyFactsWithoutCurlSaysSo(t *testing.T) {
	conn := &curlConn{noCurl: true}
	res, err := moduleIpifyFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "curl binary is required") {
		t.Errorf("res = %+v, want the missing-curl explanation", res)
	}
}
