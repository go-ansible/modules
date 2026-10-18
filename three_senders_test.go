package modules

import (
	"context"
	"strings"
	"testing"
)

// cisco_webex builds its payload keys from the VALUES of two arguments,
// which is the part most easily got wrong.
func TestCiscoWebexPayloadKeysComeFromTheArguments(t *testing.T) {
	const token = "WEBEXTOKEN-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleCiscoWebex(context.Background(), conn, map[string]any{
		"recipient_type": "toPersonEmail",
		"recipient_id":   "someone@example.com",
		"msg_type":       "markdown",
		"msg":            "**hi**",
		"personal_token": token,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	joined := strings.Join(conn.Stdins, "\n")
	for _, want := range []string{"toPersonEmail", "someone@example.com", "markdown"} {
		if !strings.Contains(joined, want) {
			t.Errorf("payload missing %q; real keys the body by the argument VALUES.\ngot %q", want, joined)
		}
	}
	// ...and not by the argument NAMES.
	for _, notWant := range []string{`recipient_type`, `msg_type`} {
		if strings.Contains(joined, notWant) {
			t.Errorf("payload contains the argument NAME %q; real uses its value as the key", notWant)
		}
	}
	assertSecretsOffArgv(t, conn, map[string]string{"personal_token": token})
}

// Real does not skip the request in check mode: it GETs /people/me and
// reports "Authentication Successful.".
func TestCiscoWebexCheckModeProbesInsteadOfSkipping(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleCiscoWebex(context.Background(), conn, map[string]any{
		"recipient_type": "roomId", "recipient_id": "r", "msg": "m",
		"personal_token": "t", CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "Authentication Successful." {
		t.Errorf("msg = %q, want real's wording", res.Msg)
	}
	if conn.calls != 1 {
		t.Errorf("made %d requests in check mode, want 1 — real probes /people/me", conn.calls)
	}
	if joined := strings.Join(conn.Stdins, "\n"); !strings.Contains(joined, "people/me") {
		t.Errorf("check mode did not hit the read-only endpoint: %q", joined)
	}
}

func TestCiscoWebexRejectsValuesRealWouldReject(t *testing.T) {
	for _, c := range []struct{ key, value string }{
		{"recipient_type", "roomID"}, // real's choice is roomId
		{"msg_type", "html"},
	} {
		t.Run(c.key+"="+c.value, func(t *testing.T) {
			conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
			args := map[string]any{
				"recipient_type": "roomId", "recipient_id": "r", "msg": "m", "personal_token": "t",
			}
			args[c.key] = c.value
			res, err := moduleCiscoWebex(context.Background(), conn, args)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Failed || !strings.Contains(res.Msg, "must be one of") {
				t.Errorf("res = %+v, want a refusal — an unchecked value becomes a JSON key", res)
			}
		})
	}
}

// Discord's execute-webhook answers 204. A 200 is a FAILURE there, and
// collapsing the two is the easy mistake.
func TestDiscordSuccessIs204Not200(t *testing.T) {
	const token = "DISCORDTOKEN-aaaaaaaaaaaa"
	for _, c := range []struct {
		status   string
		wantFail bool
	}{
		{"204", false},
		{"200", true},
	} {
		t.Run("HTTP "+c.status, func(t *testing.T) {
			conn := &curlConn{body: "\nHTTPSTATUS:" + c.status}
			res, err := moduleDiscord(context.Background(), conn, map[string]any{
				"webhook_id": "123", "webhook_token": token, "content": "hi",
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Failed != c.wantFail {
				t.Errorf("HTTP %s: failed = %v, want %v", c.status, res.Failed, c.wantFail)
			}
			if !c.wantFail && !res.Changed {
				t.Error("a successful send reports no change; real reports changed=True")
			}
			assertSecretsOffArgv(t, conn, map[string]string{"webhook_token": token})
		})
	}
}

func TestDiscordNeedsContentOrEmbeds(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:204"}
	res, err := moduleDiscord(context.Background(), conn, map[string]any{
		"webhook_id": "1", "webhook_token": "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "content, embeds") {
		t.Errorf("res = %+v, want real's required_one_of refusal", res)
	}
	if conn.calls != 0 {
		t.Errorf("sent %d request(s) with neither content nor embeds", conn.calls)
	}
}

// Real's check mode GETs the webhook, where 200 IS the success -- a
// different endpoint and a different code from the POST above.
func TestDiscordCheckModeGetsAndWants200(t *testing.T) {
	conn := &curlConn{body: `{"id":"123"}` + "\nHTTPSTATUS:200"}
	res, err := moduleDiscord(context.Background(), conn, map[string]any{
		"webhook_id": "123", "webhook_token": "t", "content": "hi", CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Errorf("res = %+v, want an unchanged success", res)
	}
	if res.Extra["http_code"] != 200 {
		t.Errorf("http_code = %v, want 200", res.Extra["http_code"])
	}
}

// google_chat puts BOTH credentials in the query string.
func TestGoogleChatKeepsQueryCredentialsOffArgv(t *testing.T) {
	const key, token = "GCKEY-aaaaaaaaaaaa", "GCTOKEN-bbbbbbbbbbbb"
	conn := &curlConn{body: `{"name":"spaces/AAA/messages/BBB"}` + "\nHTTPSTATUS:200"}
	res, err := moduleGoogleChat(context.Background(), conn, map[string]any{
		"space": "AAA", "key": key, "token": token, "text": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v, want a change", res)
	}
	if res.Extra["name"] != "spaces/AAA/messages/BBB" {
		t.Errorf("name = %v, want the response's own name", res.Extra["name"])
	}
	assertSecretsOffArgv(t, conn, map[string]string{"key": key, "token": token})
}

// Both of real's reply options, which decide whether a reply to a
// vanished thread starts a new one or errors.
func TestGoogleChatThreadReplyOptions(t *testing.T) {
	for _, c := range []struct {
		createNew bool
		want      string
	}{
		{true, "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD"},
		{false, "REPLY_MESSAGE_OR_FAIL"},
	} {
		t.Run(c.want, func(t *testing.T) {
			conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
			if _, err := moduleGoogleChat(context.Background(), conn, map[string]any{
				"space": "A", "key": "k", "token": "t", "text": "x",
				"thread_key": "THREAD-1", "create_new_thread": c.createNew,
			}); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(conn.Stdins, "\n")
			if !strings.Contains(joined, c.want) {
				t.Errorf("messageReplyOption missing %q: %q", c.want, joined)
			}
			if !strings.Contains(joined, "threadKey") {
				t.Errorf("thread.threadKey missing from the body: %q", joined)
			}
		})
	}
}

// Real's check-mode branch reports changed=TRUE and sends nothing,
// unlike most modules. Reproduced as real has it.
func TestGoogleChatCheckModeClaimsAChangeAndSendsNothing(t *testing.T) {
	conn := &curlConn{body: "{}\nHTTPSTATUS:200"}
	res, err := moduleGoogleChat(context.Background(), conn, map[string]any{
		"space": "A", "key": "k", "token": "t", "text": "x", CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("changed = false; real's check mode reports changed=True here")
	}
	if conn.calls != 0 {
		t.Errorf("made %d request(s) in check mode, want 0", conn.calls)
	}
}

func TestGoogleChatNon200UsesRealsWording(t *testing.T) {
	conn := &curlConn{body: `{"error":"nope"}` + "\nHTTPSTATUS:403"}
	res, err := moduleGoogleChat(context.Background(), conn, map[string]any{
		"space": "A", "key": "k", "token": "t", "text": "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a 403 did not fail the task")
	}
	if !strings.Contains(res.Msg, "Failed to send message to Google Chat (HTTP 403)") {
		t.Errorf("msg = %q, want real's wording", res.Msg)
	}
}
