package modules

import (
	"context"
	"io"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// pushoverConn answers the curl-presence probe and then any curl
// invocation with a scripted body, recording what reached argv and what
// reached stdin -- which is the distinction the whole design turns on.
type pushoverConn struct {
	remoteexec.Connection
	body     string
	rc       int
	noCurl   bool
	Commands []string
	Stdins   []string
}

func (c *pushoverConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	c.Commands = append(c.Commands, cmd)
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		c.Stdins = append(c.Stdins, string(b))
	}
	if strings.Contains(cmd, "command -v curl") {
		if c.noCurl {
			return remoteexec.Result{RC: 1}, nil
		}
		return remoteexec.Result{RC: 0, Stdout: "/usr/bin/curl\n"}, nil
	}
	return remoteexec.Result{RC: c.rc, Stdout: c.body}, nil
}

func (c *pushoverConn) Close() error { return nil }

const (
	poToken = "TOKEN-aaaaaaaaaaaaaaaaaaaaaaaaaa"
	poUser  = "USERKEY-bbbbbbbbbbbbbbbbbbbbbbbb"
)

func pushoverArgs() map[string]any {
	return map[string]any{
		"msg":       "it exploded",
		"app_token": poToken,
		"user_key":  poUser,
	}
}

// Real's success message is "message sent successfully: " + the raw
// body, and changed is FALSE: sending a notification is not a change.
func TestPushoverSendsAndReportsRealsMessage(t *testing.T) {
	conn := &pushoverConn{body: `{"status":1}` + "\nHTTPSTATUS:200"}
	res, err := modulePushover(context.Background(), conn, pushoverArgs())
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("failed: %+v", res)
	}
	if want := `message sent successfully: {"status":1}`; res.Msg != want {
		t.Errorf("msg = %q, want %q", res.Msg, want)
	}
	if res.Changed {
		t.Error("changed = true; sending a notification reports no change in real")
	}
}

// ⛔ The point of the design: neither credential may reach a command
// line. They are form fields, so the whole body travels on stdin
// through curl's own -K - config.
func TestPushoverKeepsBothSecretsOffTheCommandLine(t *testing.T) {
	conn := &pushoverConn{body: "ok\nHTTPSTATUS:200"}
	if _, err := modulePushover(context.Background(), conn, pushoverArgs()); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range conn.Commands {
		for name, secret := range map[string]string{"app_token": poToken, "user_key": poUser} {
			if strings.Contains(cmd, secret) {
				t.Errorf("%s reached a command line: %q", name, cmd)
			}
		}
	}
	// The CONTROL: they must be somewhere, or this test passes on a
	// module that simply never sent them.
	joined := strings.Join(conn.Stdins, "\n")
	for name, secret := range map[string]string{"app_token": poToken, "user_key": poUser} {
		if !strings.Contains(joined, secret) {
			t.Errorf("%s never reached stdin either; the request cannot have carried it", name)
		}
	}
	// And the url is on stdin too, not argv: for a webhook service the
	// URL itself is the credential, so the helper keeps it there for
	// every caller.
	if !strings.Contains(joined, "api.pushover.net") {
		t.Error("the endpoint did not reach stdin")
	}
	for _, cmd := range conn.Commands {
		if strings.Contains(cmd, "api.pushover.net") {
			t.Errorf("the endpoint reached argv: %q", cmd)
		}
	}
}

// Real's own option order, which decides the body it sends.
func TestPushoverSendsRealsFieldNamesInOrder(t *testing.T) {
	conn := &pushoverConn{body: "ok\nHTTPSTATUS:200"}
	args := pushoverArgs()
	args["pri"] = "1"
	args["title"] = "Alert!"
	args["device"] = "admins-iPhone"
	if _, err := modulePushover(context.Background(), conn, args); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(conn.Stdins, "\n")
	want := "user=" + poUser + "&token=" + poToken + "&priority=1&message=it+exploded&title=Alert%21&device=admins-iPhone"
	if !strings.Contains(joined, want) {
		t.Errorf("body not as real orders it.\n got %q\nwant it to contain %q", joined, want)
	}
}

// Real swallows the cause: fail_json(msg="Unable to send msg via
// pushover") and nothing more. The sentence is reproduced exactly,
// because a playbook may match on it; the cause is attached rather than
// discarded.
func TestPushoverNon200UsesRealsWordingAndKeepsTheCause(t *testing.T) {
	conn := &pushoverConn{body: `{"errors":["application token is invalid"]}` + "\nHTTPSTATUS:400"}
	res, err := modulePushover(context.Background(), conn, pushoverArgs())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a 400 did not fail the task")
	}
	if res.Msg != "Unable to send msg via pushover" {
		t.Errorf("msg = %q, want real's exact sentence", res.Msg)
	}
	if res.Extra["status"] != 400 {
		t.Errorf("status extra = %v, want 400", res.Extra["status"])
	}
	if s, _ := res.Extra["body"].(string); !strings.Contains(s, "application token is invalid") {
		t.Errorf("the cause was discarded; body extra = %v", res.Extra["body"])
	}
}

func TestPushoverWithoutCurlSaysWhy(t *testing.T) {
	conn := &pushoverConn{noCurl: true}
	res, err := modulePushover(context.Background(), conn, pushoverArgs())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "curl binary is required") {
		t.Errorf("res = %+v, want a failure naming curl", res)
	}
}

func TestPushoverRejectsAPriorityRealWouldReject(t *testing.T) {
	conn := &pushoverConn{body: "ok\nHTTPSTATUS:200"}
	args := pushoverArgs()
	args["pri"] = "7"
	res, err := modulePushover(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "must be one of") {
		t.Errorf("res = %+v, want a refusal; real's argument_spec restricts pri to -2..2", res)
	}
}
