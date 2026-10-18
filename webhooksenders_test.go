package modules

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// campfire's token is its BASIC AUTH username, so it would reach argv
// through curl's -u. It goes on stdin as a `user =` directive instead,
// and that is what this asserts -- alongside real's own two-request
// order: the sound first, the message second.
func TestCampfireSendsSoundThenMessageWithTheTokenOffArgv(t *testing.T) {
	const token = "CAMPFIRETOKEN-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: "\nHTTPSTATUS:201"}
	res, err := moduleCampfire(context.Background(), conn, map[string]any{
		"subscription": "acme", "token": token, "room": "123456",
		"msg": "deployed <b>v2</b>", "notify": "tada",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("changed = false; real reports changed=True")
	}
	if conn.calls != 2 {
		t.Fatalf("made %d requests, want 2 (sound then message)", conn.calls)
	}
	assertSecretsOffArgv(t, conn, map[string]string{"token": token})

	// The sound must be first: real gives up on the message if the sound
	// fails, which only means anything in that order. The presence probe
	// sends no stdin, so these two are the requests themselves.
	if len(conn.Stdins) != 2 {
		t.Fatalf("wrote stdin %d times, want one per request: %q", len(conn.Stdins), conn.Stdins)
	}
	if !strings.Contains(conn.Stdins[0], "SoundMessage") {
		t.Errorf("first request is not the sound: %q", conn.Stdins[0])
	}
	if strings.Contains(conn.Stdins[1], "SoundMessage") {
		t.Errorf("second request is a sound too: %q", conn.Stdins[1])
	}
	if !strings.Contains(conn.Stdins[1], "deployed &lt;b&gt;v2&lt;/b&gt;") {
		t.Errorf("message body not escaped as real escapes it: %q", conn.Stdins[1])
	}
	if !strings.Contains(conn.Stdins[0], `user = "`+token+`:X"`) {
		t.Errorf("token not sent as basic auth on stdin: %q", conn.Stdins[0])
	}
	if res.Extra["notify"] != "tada" || res.Extra["room"] != "123456" {
		t.Errorf("extras = %v, want real's room/msg/notify", res.Extra)
	}
}

// Real's choices list rejects a sound Campfire does not have, before any
// request goes out.
func TestCampfireRefusesAnUnknownSound(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:200"}
	res, err := moduleCampfire(context.Background(), conn, map[string]any{
		"subscription": "acme", "token": "t", "room": "1", "msg": "m",
		"notify": "airhorn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Error("an unlisted sound was accepted")
	}
	if conn.calls != 0 {
		t.Errorf("made %d request(s) for a rejected sound, want 0", conn.calls)
	}
}

// A failing sound must stop the message, and the status must appear in
// real's wording.
func TestCampfireAFailingSoundStopsTheMessage(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:404"}
	res, err := moduleCampfire(context.Background(), conn, map[string]any{
		"subscription": "acme", "token": "t", "room": "1", "msg": "m",
		"notify": "yeah",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a 404 on the sound did not fail the task")
	}
	if res.Msg != "unable to send msg: 'yeah', campfire api returned error code: '404'" {
		t.Errorf("msg = %q, want real's wording", res.Msg)
	}
	if conn.calls != 1 {
		t.Errorf("made %d requests; real returns before the message", conn.calls)
	}
}

// Without notify there is exactly one request, and notify comes back as
// null rather than "" -- real emits None.
func TestCampfireWithoutASoundSendsOnce(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:200"}
	res, err := moduleCampfire(context.Background(), conn, map[string]any{
		"subscription": "acme", "token": "t", "room": "1", "msg": "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || conn.calls != 1 {
		t.Fatalf("res = %+v, calls = %d", res, conn.calls)
	}
	if v, present := res.Extra["notify"]; !present || v != nil {
		t.Errorf("notify = %#v, want nil as real's None", v)
	}
}

// bigpanda's token is a Bearer header, and real returns it in the result
// relying on remove_values to scrub it. This port has no remove_values,
// so the token must be absent from the result as well as from argv.
func TestBigpandaPostsAndKeepsTheTokenOutOfArgvAndResult(t *testing.T) {
	const token = "BIGPANDATOKEN-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleBigpanda(context.Background(), conn, map[string]any{
		"component": "mysvc", "version": "1.2.3", "token": token,
		"state": "started", "hosts": []any{"web1"}, "env": "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("changed = false; real reports changed=True")
	}
	assertSecretsOffArgv(t, conn, map[string]string{"token": token})
	for k, v := range res.Extra {
		if s, ok := v.(string); ok && strings.Contains(s, token) {
			t.Errorf("the token is in Extra[%q]; this port has no remove_values to scrub it", k)
		}
	}
	if _, present := res.Extra["state"]; present {
		t.Error("result carries a \"state\" key; real's deployment object has none")
	}
	if res.Extra["component"] != "mysvc" || res.Extra["version"] != "1.2.3" {
		t.Errorf("extras = %v, want real's deployment object", res.Extra)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, "/data/events/deployments/start") {
		t.Errorf("wrong endpoint for state=started: %q", joined)
	}
	if !strings.Contains(joined, "Authorization: Bearer "+token) {
		t.Errorf("token not sent as a Bearer header on stdin: %q", joined)
	}
}

// state=failed hits the /end endpoint with status=failure, and
// deployment_message is renamed to message on the way out while
// travelling as errorMessage on the wire.
func TestBigpandaFailedEndsTheDeploymentAndRenamesTheMessage(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleBigpanda(context.Background(), conn, map[string]any{
		"component": "mysvc", "version": "1", "token": "t", "state": "failed",
		"hosts": []any{"web1"}, "deployment_message": "tests red",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["message"] != "tests red" {
		t.Errorf("message = %v, want real's renamed errorMessage", res.Extra["message"])
	}
	if _, present := res.Extra["errorMessage"]; present {
		t.Error("errorMessage survived in the result; real pops it")
	}
	joined := strings.Join(conn.Stdins, "\n")
	for _, want := range []string{"/data/events/deployments/end", "errorMessage", `failure`} {
		if !strings.Contains(joined, want) {
			t.Errorf("body/endpoint missing %q: %q", want, joined)
		}
	}
}

// With no hosts real uses socket.gethostname() -- which, since real runs
// on the target, is the TARGET's hostname, not ours.
func TestBigpandaDefaultsHostsToTheTargetsHostname(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200", hostname: "target-box"}
	res, err := moduleBigpanda(context.Background(), conn, map[string]any{
		"component": "c", "version": "1", "token": "t", "state": "finished",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	hosts, _ := res.Extra["hosts"].([]string)
	if len(hosts) != 1 || hosts[0] != "target-box" {
		t.Errorf("hosts = %v, want the target's own hostname", res.Extra["hosts"])
	}
	if !strings.Contains(strings.Join(conn.Stdins, "\n"), "target-box") {
		t.Error("the target's hostname did not reach the request body")
	}
}

func TestBigpandaCheckModeSendsNothing(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleBigpanda(context.Background(), conn, map[string]any{
		"component": "c", "version": "1", "token": "t", "state": "started",
		"hosts": []any{"h"}, CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("changed = false; real reports changed=True in check mode here")
	}
	if conn.calls != 0 {
		t.Errorf("made %d request(s) in check mode, want 0", conn.calls)
	}
	if !SupportsCheckMode("bigpanda") {
		t.Error("bigpanda honours a dry run but the engine would skip it")
	}
}

func TestBigpandaRefusesAnUnknownState(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleBigpanda(context.Background(), conn, map[string]any{
		"component": "c", "version": "1", "token": "t", "state": "begun",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || conn.calls != 0 {
		t.Errorf("res = %+v, calls = %d; an unlisted state must be refused", res, conn.calls)
	}
}

// rocketchat's token is two path segments of the URL, so the url itself
// is the credential -- exactly the case `url =` on stdin exists for.
func TestRocketchatPostsJSONAndKeepsTheTokenOffArgv(t *testing.T) {
	const token = "aaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb"
	conn := &curlConn{body: "\nHTTPSTATUS:200"}
	res, err := moduleRocketchat(context.Background(), conn, map[string]any{
		"domain": "chat.example.com", "token": token, "msg": "deployed",
		"channel": "ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Msg != "OK" {
		t.Fatalf("res = %+v, want real's msg \"OK\"", res)
	}
	if res.Changed {
		t.Error("changed = true; real leaves it unset")
	}
	assertSecretsOffArgv(t, conn, map[string]string{"token": token})
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, "https://chat.example.com/hooks/"+token) {
		t.Errorf("webhook url not as real builds it: %q", joined)
	}
	if !strings.Contains(joined, "Content-type: application/json") {
		t.Errorf("modern path must send real's Content-type header: %q", joined)
	}
	body := rocketchatBody(t, conn)
	if body["text"] != "deployed" {
		t.Errorf("text = %v; with color normal real sends msg as text", body["text"])
	}
	if body["channel"] != "#ops" {
		t.Errorf("channel = %v, want a # prefixed by real", body["channel"])
	}
	if body["username"] != "Ansible" || body["icon_url"] != "https://docs.ansible.com/favicon/favicon.ico" {
		t.Errorf("defaults not as real declares them: %v", body)
	}
}

// A token with no "/" is refused before any request, in real's words.
func TestRocketchatRefusesATokenWithoutASlash(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:200"}
	res, err := moduleRocketchat(context.Background(), conn, map[string]any{
		"domain": "d", "token": "nosegments", "msg": "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "Invalid Token specified, provide a valid token" {
		t.Errorf("res = %+v, want real's wording", res)
	}
	if conn.calls != 0 {
		t.Errorf("made %d request(s) for a bad token, want 0", conn.calls)
	}
}

// Any colour but "normal" moves the message into an attachment and
// drops the text field entirely -- real's own branch.
func TestRocketchatANonNormalColourMovesTheTextIntoAnAttachment(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:200"}
	if _, err := moduleRocketchat(context.Background(), conn, map[string]any{
		"domain": "d", "token": "a/b", "msg": "broken", "color": "danger",
	}); err != nil {
		t.Fatal(err)
	}
	body := rocketchatBody(t, conn)
	if _, present := body["text"]; present {
		t.Error("text survived alongside the attachment; real sets one or the other")
	}
	atts, _ := body["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("attachments = %v, want one carrying the message", body["attachments"])
	}
	first, _ := atts[0].(map[string]any)
	if first["text"] != "broken" || first["color"] != "danger" {
		t.Errorf("attachment = %v, want real's {text, color}", first)
	}
}

// An attachment with no fallback gets its own text copied into one, and
// the colour attachment stays first.
func TestRocketchatDefaultsAFallbackAndKeepsTheOrder(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:200"}
	if _, err := moduleRocketchat(context.Background(), conn, map[string]any{
		"domain": "d", "token": "a/b", "msg": "first", "color": "good",
		"attachments": []any{map[string]any{"text": "second"}},
	}); err != nil {
		t.Fatal(err)
	}
	atts, _ := rocketchatBody(t, conn)["attachments"].([]any)
	if len(atts) != 2 {
		t.Fatalf("attachments = %v, want the colour one then mine", atts)
	}
	if a, _ := atts[0].(map[string]any); a["text"] != "first" {
		t.Errorf("first attachment = %v, want the message's own", a)
	}
	mine, _ := atts[1].(map[string]any)
	if mine["fallback"] != "second" {
		t.Errorf("fallback = %v, want text copied in by real", mine["fallback"])
	}
}

// An emoji REPLACES the url in real, rather than joining it.
func TestRocketchatAnEmojiReplacesTheIconURL(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:200"}
	if _, err := moduleRocketchat(context.Background(), conn, map[string]any{
		"domain": "d", "token": "a/b", "msg": "m", "icon_emoji": ":tada:",
		"icon_url": "https://example.com/i.png",
	}); err != nil {
		t.Fatal(err)
	}
	body := rocketchatBody(t, conn)
	if body["icon_emoji"] != ":tada:" {
		t.Errorf("icon_emoji = %v", body["icon_emoji"])
	}
	if _, present := body["icon_url"]; present {
		t.Error("icon_url survived; real sends the emoji INSTEAD of it")
	}
}

// Pre-7.4.0 changes the encoding, not just a header: the JSON goes raw
// behind "payload=" and no Content-Type is sent at all.
func TestRocketchatPre740WrapsThePayloadAndWithholdsTheContentType(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:200"}
	if _, err := moduleRocketchat(context.Background(), conn, map[string]any{
		"domain": "d", "token": "a/b", "msg": "m", "is_pre740": true,
	}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, `data = "payload={`) {
		t.Errorf("body is not real's raw payload= form: %q", joined)
	}
	if !strings.Contains(joined, `header = "Content-Type:"`) {
		t.Errorf("curl was not told to withhold its own Content-Type: %q", joined)
	}
	if strings.Contains(joined, "Content-type: application/json") {
		t.Errorf("pre-7.4.0 must not send the JSON header: %q", joined)
	}
}

func TestRocketchatNon200Fails(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:500"}
	res, err := moduleRocketchat(context.Background(), conn, map[string]any{
		"domain": "d", "token": "a/b", "msg": "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "failed to send message, return status=500" {
		t.Errorf("res = %+v, want real's wording", res)
	}
}

// rocketchatBody pulls the JSON document out of the curl config the
// module wrote to stdin, so the assertions above read fields rather
// than matching substrings of a serialisation whose key order is not
// ours to fix.
func rocketchatBody(t *testing.T, conn *curlConn) map[string]any {
	t.Helper()
	joined := strings.Join(conn.Stdins, "\n")
	const marker = `data = "`
	i := strings.Index(joined, marker)
	if i < 0 {
		t.Fatalf("no body was sent: %q", joined)
	}
	rest := joined[i+len(marker):]
	j := strings.Index(rest, "\"\n")
	if j < 0 {
		j = len(rest)
	}
	raw := strings.TrimPrefix(curlConfigUnescape(rest[:j]), "payload=")
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("body is not the JSON real sends: %v in %q", err, raw)
	}
	return body
}
