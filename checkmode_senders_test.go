package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryCheckModeImplementationIsRegistered is the invariant whose
// absence let four modules implement a dry run that the engine never
// reached. checkModeSupport is opt-in by design -- an unported module
// must be SKIPPED rather than run for real -- but the cost of that
// default is this: a module can grow a correct check-mode branch and
// still be skipped, and nothing says so. Both halves are silent, which
// is why this reads the package's own sources rather than trusting a
// hand-kept list.
//
// It can only flag what it can NAME: a module implemented in a file
// called something else (shell, which lives in command.go) is invisible
// to it. It would still have caught all four of the misses it was
// written for -- cisco_webex, discord, google_chat and telegram -- and
// an under-approximation that fires is worth more than a complete one
// that cannot run.
func TestEveryCheckModeImplementationIsRegistered(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	registered := make(map[string]bool)
	for _, name := range Default().Names() {
		registered[name] = true
	}

	var checked int
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "checkmode.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), "InCheckMode(args)") {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(f), ".go")
		if !registered[name] {
			// A helper, or a module whose file is named differently;
			// there is nothing to assert about a name that is not one.
			continue
		}
		checked++
		if !SupportsCheckMode(name) {
			t.Errorf("%s branches on check mode in %s but is absent from checkModeSupport, "+
				"so the engine skips it instead of running the branch", name, f)
		}
	}
	// The positive control: a scan that cannot see is indistinguishable
	// from a scan that found nothing. There are at least a dozen such
	// modules, so a count in single digits means the glob or the marker
	// stopped matching, not that the gap closed.
	if checked < 10 {
		t.Errorf("only %d module(s) examined; the source scan has stopped seeing check-mode branches", checked)
	}
}

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
