package modules

import (
	"context"
	"io"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// curlConn answers the curl-presence probe and then any curl invocation
// with a scripted body, recording what reached argv and what reached
// stdin. That distinction is what every module built on curlDo has to
// get right, so the fake is shared rather than written per module.
type curlConn struct {
	remoteexec.Connection
	body     string // may carry the \nHTTPSTATUS: trailer
	rc       int
	noCurl   bool
	hostname string // answer for a bare `hostname`, when a module asks
	Commands []string
	Stdins   []string
	calls    int
}

func (c *curlConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	c.Commands = append(c.Commands, cmd)
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		c.Stdins = append(c.Stdins, string(b))
	}
	if cmd == "hostname" {
		// bigpanda asks the TARGET for its name; an empty answer here
		// would be indistinguishable from the module not asking.
		if c.hostname == "" {
			return remoteexec.Result{RC: 1, Stderr: "no hostname scripted on this fake"}, nil
		}
		return remoteexec.Result{RC: 0, Stdout: c.hostname + "\n"}, nil
	}
	if strings.Contains(cmd, "command -v curl") {
		if c.noCurl {
			return remoteexec.Result{RC: 1}, nil
		}
		return remoteexec.Result{RC: 0, Stdout: "/usr/bin/curl\n"}, nil
	}
	c.calls++
	return remoteexec.Result{RC: c.rc, Stdout: c.body}, nil
}

func (c *curlConn) Close() error { return nil }

// assertSecretsOffArgv is the check these modules exist to satisfy: a
// credential may travel on stdin, never on a command line. It carries
// its own control -- each secret must appear on stdin -- so it cannot
// pass on a module that simply never sent them.
func assertSecretsOffArgv(t *testing.T, conn *curlConn, secrets map[string]string) {
	t.Helper()
	for _, cmd := range conn.Commands {
		for name, secret := range secrets {
			if strings.Contains(cmd, secret) {
				t.Errorf("%s reached a command line: %q", name, cmd)
			}
		}
	}
	joined := strings.Join(conn.Stdins, "\n")
	for name, secret := range secrets {
		if !strings.Contains(joined, secret) {
			t.Errorf("%s never reached stdin either; the request cannot have carried it", name)
		}
	}
}

// grove puts channel_token in the URL PATH, so the url itself is a
// credential -- the case that makes sending it on stdin matter.
func TestGrovePostsAndKeepsTheTokenOffArgv(t *testing.T) {
	const token = "GROVETOKEN-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: "ok\nHTTPSTATUS:200"}
	res, err := moduleGrove(context.Background(), conn, map[string]any{
		"channel_token":   token,
		"message_content": "deployed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Msg != "OK" {
		t.Errorf("res = %+v, want real's msg \"OK\"", res)
	}
	if res.Changed {
		t.Error("changed = true; real leaves it unset for a notification")
	}
	assertSecretsOffArgv(t, conn, map[string]string{"channel_token": token})
	if joined := strings.Join(conn.Stdins, "\n"); !strings.Contains(joined, "service=ansible&message=deployed") {
		t.Errorf("body not as real orders it: %q", joined)
	}
}

func TestGroveNon200Fails(t *testing.T) {
	conn := &curlConn{body: "nope\nHTTPSTATUS:403"}
	res, err := moduleGrove(context.Background(), conn, map[string]any{
		"channel_token": "t", "message_content": "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "failed to send notification") {
		t.Errorf("res = %+v, want real's wording", res)
	}
}

// nexmo is a GET with both credentials in the QUERY STRING, and it
// sends to every destination -- real's own loop returns after the
// first, which moduleNexmo's doc explains is a bug, not a contract.
func TestNexmoGetsOncePerDestination(t *testing.T) {
	const key, secret = "NEXMOKEY-aaaa", "NEXMOSECRET-bbbb"
	conn := &curlConn{body: `{"messages":[{"status":"0"}]}` + "\nHTTPSTATUS:200"}
	res, err := moduleNexmo(context.Background(), conn, map[string]any{
		"api_key": key, "api_secret": secret, "src": "15551234567",
		"dest": []any{"15557654321", "15550000000"}, "msg": "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Error("changed = true; real reports changed=False")
	}
	responses, _ := res.Extra["responses"].(map[string]any)
	if len(responses) != 2 {
		t.Errorf("responses has %d entries, want one per destination: %v", len(responses), responses)
	}
	if conn.calls != 2 {
		t.Errorf("made %d requests, want 2 — real's own loop returns after the first", conn.calls)
	}
	assertSecretsOffArgv(t, conn, map[string]string{"api_key": key, "api_secret": secret})
}

func TestNexmoAPerMessageStatusOtherThanZeroFails(t *testing.T) {
	conn := &curlConn{body: `{"messages":[{"status":"4"}]}` + "\nHTTPSTATUS:200"}
	res, err := moduleNexmo(context.Background(), conn, map[string]any{
		"api_key": "k", "api_secret": "s", "src": "1", "dest": []any{"2"}, "msg": "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Error("a per-message status of 4 did not fail the task")
	}
	if res.Msg != "One or messages failed to send" {
		t.Errorf("msg = %q, want real's wording (typo included)", res.Msg)
	}
}

// telegram posts JSON, reports changed=TRUE on success (unlike the
// other two), and supports check mode.
func TestTelegramPostsJSONAndReportsChanged(t *testing.T) {
	const token = "BOTTOKEN-123456:aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: `{"ok":true}` + "\nHTTPSTATUS:200"}
	res, err := moduleTelegram(context.Background(), conn, map[string]any{
		"token":    token,
		"api_args": map[string]any{"chat_id": "-1001", "text": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("changed = false; real reports changed=True on a successful send")
	}
	assertSecretsOffArgv(t, conn, map[string]string{"token": token})
	joined := strings.Join(conn.Stdins, "\n")
	for _, want := range []string{"chat_id", "hello", "parse_mode"} {
		if !strings.Contains(joined, want) {
			t.Errorf("body missing %q; real seeds chat_id/parse_mode/text.\ngot %q", want, joined)
		}
	}
}

// Real deletes parse_mode entirely when it is the string "plain".
func TestTelegramDropsPlainParseMode(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	if _, err := moduleTelegram(context.Background(), conn, map[string]any{
		"token":    "t",
		"api_args": map[string]any{"parse_mode": "plain", "text": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(conn.Stdins, "\n"); strings.Contains(joined, "parse_mode") {
		t.Errorf("parse_mode survived; real deletes it when it is \"plain\": %q", joined)
	}
}

func TestTelegramCheckModeSendsNothing(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleTelegram(context.Background(), conn, map[string]any{
		"token": "t", "api_args": map[string]any{"text": "x"},
		CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Errorf("res = %+v, want an unchanged, unfailed no-op", res)
	}
	if conn.calls != 0 {
		t.Errorf("made %d request(s) in check mode, want 0", conn.calls)
	}
}

// Real's failure message embeds the url, and real gets away with it:
// remove_values scrubs the no_log token out of any result string before
// it prints. This port has no remove_values -- only task-level no_log,
// which suppresses a whole result -- so the same sentence here would
// emit the token verbatim. Hence the url is omitted, and this holds
// that line.
func TestTelegramFailureKeepsTheTokenOutOfTheMessage(t *testing.T) {
	const token = "BOTTOKEN-leakme:cccccccccccccccc"
	conn := &curlConn{body: `{"description":"chat not found"}` + "\nHTTPSTATUS:400"}
	res, err := moduleTelegram(context.Background(), conn, map[string]any{
		"token": token, "api_args": map[string]any{"chat_id": "-1", "text": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a 400 did not fail the task")
	}
	if strings.Contains(res.Msg, token) {
		t.Errorf("the bot token is in the failure message; this port has no remove_values to scrub it: %q", res.Msg)
	}
	for k, v := range res.Extra {
		if s, ok := v.(string); ok && strings.Contains(s, token) {
			t.Errorf("the bot token is in Extra[%q]: %q", k, s)
		}
	}
	if res.Extra["telegram_error"] != "chat not found" {
		t.Errorf("telegram_error = %v, want real's description field", res.Extra["telegram_error"])
	}
}

// curlConfigUnescape is curlConfigEscape's inverse, so a test can read
// back the body the module wrote rather than matching substrings of it.
func curlConfigUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
