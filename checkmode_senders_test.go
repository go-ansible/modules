package modules

import (
	"context"
	"strings"
	"testing"
)

// The invariant that used to live here -- every module branching on
// check mode must be registered -- moved to
// checkmode_registered_test.go, where it asks each registered module
// about its own FUNCTION BODY instead of matching a file's basename
// against a module name. pagerduty.go is why: it holds three modules,
// only two of which branch, and the file-name version named the third.
//
// The original wording, kept because it is still the reason the
// invariant exists: checkModeSupport is opt-in by design -- an unported
// module must be SKIPPED rather than run for real -- but the cost of
// that default is this: a module can grow a correct check-mode branch
// and still be skipped, and nothing says so. Both halves are silent.

// Real cisco_webex does NOT skip in check mode: it swaps the POST for a
// read-only GET of /people/me and reports whether the token
// authenticates. That is a different claim from "sends nothing", and
// registering the module is what makes it reachable at all.
func TestCiscoWebexCheckModeProbesInsteadOfPosting(t *testing.T) {
	const token = "WEBEXTOKEN-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleCiscoWebex(context.Background(), conn, map[string]any{
		"recipient_type": "toPersonEmail", "recipient_id": "a@b.c",
		"msg_type": "text", "msg": "hello", "personal_token": token,
		CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Msg != "Authentication Successful." {
		t.Errorf("msg = %q, want real's own check-mode wording", res.Msg)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, "https://webexapis.com/v1/people/me") {
		t.Errorf("did not probe real's own url: %q", joined)
	}
	if strings.Contains(joined, "/v1/messages") {
		t.Errorf("a dry run reached the message endpoint: %q", joined)
	}
	if strings.Contains(joined, "hello") {
		t.Errorf("the message body was sent in check mode: %q", joined)
	}
	assertSecretsOffArgv(t, conn, map[string]string{"personal_token": token})
	if !SupportsCheckMode("cisco_webex") {
		t.Error("cisco_webex honours a dry run but the engine would skip it")
	}
}

// Real discord GETs the webhook to validate it and reports
// changed=FALSE -- unlike google_chat, which reports a change.
func TestDiscordCheckModeValidatesTheWebhook(t *testing.T) {
	const webhookToken = "DISCORDTOKEN-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleDiscord(context.Background(), conn, map[string]any{
		"webhook_id": "123", "webhook_token": webhookToken,
		"content": "hello", CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Error("changed = true; real reports changed=False for the check-mode probe")
	}
	if res.Extra["http_code"] != 200 {
		t.Errorf("http_code = %v, want real's own key", res.Extra["http_code"])
	}
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, `request = "GET"`) {
		t.Errorf("the probe was not a GET: %q", joined)
	}
	if strings.Contains(joined, "hello") {
		t.Errorf("the message content was sent in check mode: %q", joined)
	}
	assertSecretsOffArgv(t, conn, map[string]string{"webhook_token": webhookToken})
	if !SupportsCheckMode("discord") {
		t.Error("discord honours a dry run but the engine would skip it")
	}
}

// Real google_chat reports changed=True and sends nothing at all.
func TestGoogleChatCheckModeSendsNothing(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleGoogleChat(context.Background(), conn, map[string]any{
		"space": "spaces/AAAA", "key": "k", "token": "t", "text": "hello",
		CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("changed = false; real exits with changed=True")
	}
	if conn.calls != 0 {
		t.Errorf("made %d request(s) in check mode, want 0", conn.calls)
	}
	if !SupportsCheckMode("google_chat") {
		t.Error("google_chat honours a dry run but the engine would skip it")
	}
}

// telegram's own check-mode test lives beside the other telegram tests;
// this asserts only the half that was missing -- that the engine can
// reach it.
func TestTelegramIsReachableInCheckMode(t *testing.T) {
	if !SupportsCheckMode("telegram") {
		t.Error("telegram honours a dry run but the engine would skip it")
	}
}
