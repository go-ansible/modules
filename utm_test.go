package modules

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// utmBodies returns every JSON body the module sent, in order, read back
// out of the curl config written to stdin.
func utmBodies(t *testing.T, conn *curlConn) []map[string]any {
	t.Helper()
	const marker = `data = "`
	var out []map[string]any
	for _, in := range conn.Stdins {
		i := strings.Index(in, marker)
		if i < 0 {
			continue
		}
		rest := in[i+len(marker):]
		j := strings.Index(rest, "\"\n")
		if j < 0 {
			j = len(rest)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(curlConfigUnescape(rest[:j])), &body); err != nil {
			t.Fatalf("a request body is not JSON: %v in %q", err, rest[:j])
		}
		out = append(out, body)
	}
	return out
}

func utmArgs(extra map[string]any) map[string]any {
	args := map[string]any{
		"utm_host":  "fw.example.com",
		"utm_token": "UTMTOKEN-aaaaaaaaaaaaaaaa",
		"name":      "web",
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// The device has no by-name endpoint, so every operation starts with a
// GET of the whole collection. A name that is not in it is created.
func TestUTMCreatesWhenTheNameIsAbsent(t *testing.T) {
	conn := &curlConn{body: `[{"name":"other","_ref":"REF_OTHER"}]` + "\nHTTPSTATUS:200"}
	res, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(map[string]any{
		"hostname": "web.example.com",
		"comment":  "the web host",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("changed = false; creating an object is a change")
	}
	if conn.calls != 2 {
		t.Fatalf("made %d requests, want the lookup and the create", conn.calls)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, "https://fw.example.com:4444/api/objects/network/dns_host/") {
		t.Errorf("url is not real's shape, port default included: %q", joined)
	}
	if !strings.Contains(joined, `request = "POST"`) {
		t.Errorf("the second request is not a POST: %q", joined)
	}

	bodies := utmBodies(t, conn)
	if len(bodies) != 1 {
		t.Fatalf("sent %d bodies, want one (the lookup carries none)", len(bodies))
	}
	body := bodies[0]
	// Real's defaults, transcribed from utm_dns_host.py.
	for k, want := range map[string]any{
		"address":   "0.0.0.0",
		"address6":  "::",
		"interface": "",
		"resolved":  false,
		"resolved6": false,
		"timeout":   float64(0),
		"hostname":  "web.example.com",
		"comment":   "the web host",
		"name":      "web",
	} {
		if body[k] != want {
			t.Errorf("body[%q] = %#v, want %#v", k, body[k], want)
		}
	}
}

// Real sends module.params as the body, which still holds the token and
// the url_username/url_password pair its own helper injected. That is
// reproduced deliberately -- see utm.go -- so the test states it rather
// than leaving a future reader to think it a leak.
func TestUTMBodyCarriesRealsOwnCredentialFields(t *testing.T) {
	const token = "UTMTOKEN-bbbbbbbbbbbbbbbb"
	conn := &curlConn{body: "[]\nHTTPSTATUS:200"}
	if _, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(map[string]any{
		"utm_token": token,
	})); err != nil {
		t.Fatal(err)
	}
	body := utmBodies(t, conn)[0]
	if body["utm_token"] != token || body["url_password"] != token || body["url_username"] != "token" {
		t.Errorf("body lost real's own credential fields: utm_token=%v url_username=%v url_password=%v",
			body["utm_token"], body["url_username"], body["url_password"])
	}
	// And the thing that actually matters: none of it on a command line.
	assertSecretsOffArgv(t, conn, map[string]string{"utm_token": token})
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, `user = "token:`+token+`"`) {
		t.Errorf("basic auth is not real's token user on stdin: %q", joined)
	}
}

// An existing object whose change-relevant keys all match is left alone,
// and real reports changed=false with the object it found.
func TestUTMUnchangedWhenTheRelevantKeysMatch(t *testing.T) {
	existing := `[{"name":"web","_ref":"REF_WEB","comment":"c","hostname":"h","interface":"eth0"}]`
	conn := &curlConn{body: existing + "\nHTTPSTATUS:200"}
	res, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(map[string]any{
		"comment": "c", "hostname": "h", "interface": "eth0",
		// A field real does NOT list as change-relevant: changing it
		// must not trigger a PUT.
		"address": "10.0.0.9",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Error("changed = true; every change-relevant key matches")
	}
	if conn.calls != 1 {
		t.Errorf("made %d requests, want only the lookup", conn.calls)
	}
	if res.Extra["result"] == nil {
		t.Error("result is empty; real returns the object it compared against")
	}
}

// A differing change-relevant key updates in place, with the device's
// own _ref appended to the collection url.
func TestUTMUpdatesThroughTheRefWhenARelevantKeyDiffers(t *testing.T) {
	existing := `[{"name":"web","_ref":"REF_WEB","comment":"old","hostname":"h","interface":""}]`
	conn := &curlConn{body: existing + "\nHTTPSTATUS:200"}
	res, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(map[string]any{
		"comment": "new", "hostname": "h",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v, want a change", res)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, `request = "PUT"`) {
		t.Errorf("not a PUT: %q", joined)
	}
	if !strings.Contains(joined, "/api/objects/network/dns_host/REF_WEB") {
		t.Errorf("the device's _ref is not in the url: %q", joined)
	}
}

// state: absent deletes through the _ref, with the acknowledgement
// header real sends, and is a no-op when the object is already gone.
func TestUTMAbsent(t *testing.T) {
	conn := &curlConn{body: `[{"name":"web","_ref":"REF_WEB"}]` + "\nHTTPSTATUS:200"}
	res, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(map[string]any{
		"state": "absent",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v, want a change", res)
	}
	joined := strings.Join(conn.Stdins, "\n")
	for _, want := range []string{`request = "DELETE"`, "REF_WEB", "X-Restd-Err-Ack: all"} {
		if !strings.Contains(joined, want) {
			t.Errorf("delete missing %q: %q", want, joined)
		}
	}

	gone := &curlConn{body: "[]\nHTTPSTATUS:200"}
	res2, err := Default().Run(context.Background(), "utm_dns_host", gone, utmArgs(map[string]any{
		"state": "absent",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Failed || res2.Changed {
		t.Errorf("res = %+v; removing an absent object changes nothing", res2)
	}
	if gone.calls != 1 {
		t.Errorf("made %d requests for an absent object, want only the lookup", gone.calls)
	}
}

// An info module only ever GETs, reports changed=false, and omits the
// result key entirely when the object does not exist -- which is real's
// own exit_json(changed=False) with nothing else in it.
func TestUTMInfoOnlyReads(t *testing.T) {
	conn := &curlConn{body: `[{"name":"web","_ref":"REF_WEB","address":"10.0.0.1"}]` + "\nHTTPSTATUS:200"}
	res, err := Default().Run(context.Background(), "utm_network_interface_address_info", conn, utmArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want an unchanged read", res)
	}
	if conn.calls != 1 {
		t.Errorf("made %d requests, want one GET", conn.calls)
	}
	if !strings.Contains(strings.Join(conn.Stdins, "\n"), "/api/objects/network/interface_address/") {
		t.Error("the info module used the wrong endpoint")
	}

	missing := &curlConn{body: "[]\nHTTPSTATUS:200"}
	res2, err := Default().Run(context.Background(), "utm_network_interface_address_info", missing, utmArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Failed || res2.Changed {
		t.Fatalf("res = %+v", res2)
	}
	if _, present := res2.Extra["result"]; present {
		t.Errorf("result key present for an absent object: %v", res2.Extra["result"])
	}

	// All five info modules are reachable under --check, because real
	// declares supports_check_mode=True for each and a read changes
	// nothing.
	for _, name := range []string{
		"utm_aaa_group_info", "utm_ca_host_key_cert_info",
		"utm_network_interface_address_info", "utm_proxy_frontend_info",
		"utm_proxy_location_info",
	} {
		if !SupportsCheckMode(name) {
			t.Errorf("%s is read-only but the engine would skip it in check mode", name)
		}
	}
	// And the write modules are not.
	for _, name := range []string{"utm_dns_host", "utm_network_interface_address", "utm_ca_host_key_cert"} {
		if SupportsCheckMode(name) {
			t.Errorf("%s writes, and real declares no check-mode support for it", name)
		}
	}
}

// A 4xx on any step fails the task rather than being read as "no such
// object" -- the distinction real draws with `info["status"] >= 400`
// before it ever looks at the body.
func TestUTMAnErrorIsNotAnAbsence(t *testing.T) {
	conn := &curlConn{body: `{"error":"forbidden"}` + "\nHTTPSTATUS:403"}
	res, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a 403 on the lookup was treated as an empty collection")
	}
	if !strings.Contains(res.Msg, "403") {
		t.Errorf("msg = %q, want the status in it", res.Msg)
	}
	if conn.calls != 1 {
		t.Errorf("made %d requests after a 403, want 1", conn.calls)
	}
}

// The case that makes the status check load-bearing rather than
// decorative: a 403 whose body is a VALID EMPTY ARRAY. Parsing alone
// cannot tell that from "no such object", so a module that looked only
// at the body would conclude the object is absent and POST a new one --
// writing to a firewall straight after an authentication failure.
func TestUTMAForbiddenEmptyListDoesNotCreate(t *testing.T) {
	conn := &curlConn{body: "[]\nHTTPSTATUS:403"}
	res, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a 403 with an empty array body was read as an absent object")
	}
	if res.Changed {
		t.Error("changed = true after a 403: something was written")
	}
	if conn.calls != 1 {
		t.Errorf("made %d requests, want only the refused lookup", conn.calls)
	}
	if !strings.Contains(res.Msg, "403") {
		t.Errorf("msg = %q, want the status in it", res.Msg)
	}
}

// Real compares with result[key] and raises a KeyError when the device's
// object has no such field. That becomes a message naming the field,
// which is the same failure with the useful half kept.
func TestUTMAMissingComparisonFieldIsNamed(t *testing.T) {
	conn := &curlConn{body: `[{"name":"web","_ref":"REF_WEB","comment":"c"}]` + "\nHTTPSTATUS:200"}
	res, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(map[string]any{
		"comment": "c",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a device object missing a change-relevant key did not fail")
	}
	if !strings.Contains(res.Msg, "hostname") {
		t.Errorf("msg = %q, want the missing field named", res.Msg)
	}
}

// The response is echoed back with the junk real sent, and real strips
// those keys before reporting.
func TestUTMCleansTheEchoedRequestOutOfTheResult(t *testing.T) {
	created := `{"name":"web","_ref":"REF_NEW","utm_host":"fw.example.com","utm_token":"t",` +
		`"utm_port":4444,"utm_protocol":"https","validate_certs":true,` +
		`"url_username":"token","url_password":"t","state":"present","address":"0.0.0.0"}`
	conn := &curlConn{body: "[]\nHTTPSTATUS:200", postBody: created + "\nHTTPSTATUS:201"}
	res, err := Default().Run(context.Background(), "utm_dns_host", conn, utmArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	result, _ := res.Extra["result"].(map[string]any)
	if result == nil {
		t.Fatalf("result = %#v, want the created object", res.Extra["result"])
	}
	for _, k := range []string{
		"utm_host", "utm_port", "utm_token", "utm_protocol",
		"validate_certs", "url_username", "url_password", "state",
	} {
		if _, present := result[k]; present {
			t.Errorf("result still carries %q, which real deletes", k)
		}
	}
	if result["name"] != "web" || result["address"] != "0.0.0.0" {
		t.Errorf("result lost the object's own fields: %v", result)
	}
}
