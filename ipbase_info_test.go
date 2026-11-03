package modules

import (
	"context"
	"strings"
	"testing"
)

// ipbase.com answers `{"data": {...}}`, and real's
// `exit_json(**ipbase.info())` makes that outer object the result's own
// keys -- so `data` is the key a playbook reads, which is what real's
// RETURN block documents.
const ipbaseAnswer = `{"data":{"ip":"1.1.1.1","hostname":"one.one.one.one","type":"v4",` +
	`"location":{"country":{"alpha2":"US","name":"United States"}}}}`

func TestIpbaseInfoFlattensTheResponsesOwnKeys(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(ipbaseAnswer)}}
	res, err := moduleIpbaseInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Error("changed = true; real reports changed=False")
	}
	data, ok := res.Extra["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want the API's own object under the key real returns", res.Extra["data"])
	}
	if data["ip"] != "1.1.1.1" {
		t.Errorf("ip = %v, want the API's own value", data["ip"])
	}
	// No arguments given: real still sends language, because its default
	// is "en" and the truth test on it passes.
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, "https://api.ipbase.com/v2/info?language=en") {
		t.Errorf("request was not real's default: %q", joined)
	}
	if strings.Contains(joined, "hostname=") {
		t.Error("hostname was sent though it defaults to false")
	}
}

// The query string is real's, parameter for parameter and in real's own
// insertion order -- ip, apikey, hostname, language. Order is not
// cosmetic here: it is what a test can assert and what makes a captured
// request comparable with real's.
func TestIpbaseInfoBuildsRealsQueryString(t *testing.T) {
	const key = "IPBASEKEY-aaaaaaaaaaaaaaaa"
	conn := &curlConn{bodies: []string{ok200(ipbaseAnswer)}}
	if _, err := moduleIpbaseInfo(context.Background(), conn, map[string]any{
		"ip": "8.8.8.8", "apikey": key, "hostname": true, "language": "de",
	}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(conn.Stdins, "\n")
	want := "https://api.ipbase.com/v2/info?ip=8.8.8.8&apikey=" + key + "&hostname=1&language=de"
	if !strings.Contains(joined, want) {
		t.Errorf("query string is not real's:\n got %q\nwant %q", joined, want)
	}
	// hostname is the integer 1, not "true" -- real sends `1`.
	if strings.Contains(joined, "hostname=true") {
		t.Error("hostname sent as a boolean; real sends 1")
	}
	assertSecretsOffArgv(t, conn, map[string]string{"apikey": key})
}

func TestIpbaseInfoSendsRealsHeaders(t *testing.T) {
	conn := &curlConn{bodies: []string{ok200(ipbaseAnswer)}}
	if _, err := moduleIpbaseInfo(context.Background(), conn, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(conn.Stdins, "\n")
	for _, want := range []string{
		`header = "Accept: application/json"`,
		`header = "User-Agent: ansible-community.general.ipbase_info/0.1.0"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in:\n%s", want, joined)
		}
	}
	// Real hardcodes timeout=10; it is not an argument.
	if !strings.Contains(strings.Join(conn.Commands, " "), "--max-time 10") {
		t.Errorf("timeout is not real's 10: %q", conn.Commands)
	}
}

func TestIpbaseInfoNon200CarriesRealsSentence(t *testing.T) {
	conn := &curlConn{bodies: []string{`{"message":"nope"}` + "\nHTTPSTATUS:429"}}
	res, err := moduleIpbaseInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	want := "The API request to ipbase.com returned an error status code 429"
	if !res.Failed || res.Msg != want {
		t.Errorf("msg = %q, want real's wording %q", res.Msg, want)
	}
}

// TestIpbaseInfoDoesNotLeakTheApiKeyInAFailureMessage is the one place
// this module does not quote real word for word, and the reason is that
// real is protected by something this package does not have.
//
// Real's parse-failure message interpolates the whole URL, apikey and
// all, and AnsibleModule scrubs every no_log value out of a message
// before printing it. Nothing here scrubs anything, so the literal
// translation would print the key into the playbook's output.
func TestIpbaseInfoDoesNotLeakTheApiKeyInAFailureMessage(t *testing.T) {
	const key = "IPBASEKEY-bbbbbbbbbbbbbbbb"
	conn := &curlConn{bodies: []string{ok200("<html>not json at all</html>")}}
	res, err := moduleIpbaseInfo(context.Background(), conn, map[string]any{
		"ip": "8.8.8.8", "apikey": key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want a failure on an unparseable body", res)
	}
	if strings.Contains(res.Msg, key) {
		t.Errorf("the apikey reached the module's own message: %q", res.Msg)
	}
	// Controls, both halves. The message must still be real's sentence,
	// must still name the URL, and must still say a key was sent --
	// dropping the parameter would be a different lie from printing it.
	if !strings.HasPrefix(res.Msg, "Failed to parse the ipbase.com response: ") {
		t.Errorf("msg = %q, want real's sentence", res.Msg)
	}
	for _, want := range []string{"apikey=REDACTED", "ip=8.8.8.8", "not json at all"} {
		if !strings.Contains(res.Msg, want) {
			t.Errorf("msg = %q, want it to contain %q", res.Msg, want)
		}
	}
}

func TestIpbaseInfoWithoutCurlSaysSo(t *testing.T) {
	conn := &curlConn{noCurl: true}
	res, err := moduleIpbaseInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "curl binary is required") {
		t.Errorf("res = %+v, want the missing-curl explanation", res)
	}
}
