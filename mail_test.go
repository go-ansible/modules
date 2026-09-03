package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestMailExtractAddr(t *testing.T) {
	cases := map[string]string{
		"john.smith@example.com":              "john.smith@example.com",
		"John Smith <john.smith@example.com>": "john.smith@example.com",
		"  root  ":                            "root",
		"Jane Jolie <jane@example.net>":       "jane@example.net",
	}
	for in, want := range cases {
		if got := mailExtractAddr(in); got != want {
			t.Errorf("mailExtractAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMailCurlCmdSecureModes(t *testing.T) {
	cases := []struct {
		secure   string
		wantURL  string
		wantFlag string
		noFlag   string
	}{
		{"never", "smtp://host:25", "", "--ssl"},
		{"try", "smtp://host:25", "--ssl", "--ssl-reqd"},
		{"starttls", "smtp://host:25", "--ssl-reqd", ""},
		{"always", "smtps://host:25", "", "--ssl-reqd"},
	}
	for _, c := range cases {
		cmd := mailCurlCmd("host", 25, "root", []string{"root"}, "", "", c.secure, 20)
		if !strings.Contains(cmd, "--url "+shellQuote(c.wantURL)) {
			t.Errorf("secure=%s: cmd = %q, want url %q", c.secure, cmd, c.wantURL)
		}
		if c.wantFlag != "" && !strings.Contains(cmd, c.wantFlag) {
			t.Errorf("secure=%s: cmd = %q, want flag %q", c.secure, cmd, c.wantFlag)
		}
		if c.noFlag != "" && strings.Contains(cmd, c.noFlag) {
			t.Errorf("secure=%s: cmd = %q, want NOT to contain %q", c.secure, cmd, c.noFlag)
		}
	}
}

func TestMailCurlCmdAuth(t *testing.T) {
	cmd := mailCurlCmd("host", 587, "root", []string{"root"}, "alice", "s3cret", "try", 20)
	if !strings.Contains(cmd, "--user "+shellQuote("alice:s3cret")) {
		t.Errorf("cmd = %q, want --user alice:s3cret", cmd)
	}
}

func TestMailCurlCmdNoAuth(t *testing.T) {
	cmd := mailCurlCmd("host", 25, "root", []string{"root"}, "", "", "try", 20)
	if strings.Contains(cmd, "--user") {
		t.Errorf("cmd = %q, want no --user flag when username is empty", cmd)
	}
}

func TestMailBuildMessageHeadersAndBcc(t *testing.T) {
	msg := mailBuildMessage(
		"jane@example.net (Jane Jolie)",
		[]string{"John Doe <j.d@example.org>"},
		[]string{"Charlie Root <root@localhost>"},
		"Ansible-report", "Hello there", "plain", "us-ascii",
		[]string{"Reply-To=john@example.com", "X-Special=Something or other"},
		"ansible",
	)
	for _, want := range []string{
		"From: jane@example.net (Jane Jolie)\r\n",
		"To: John Doe <j.d@example.org>\r\n",
		"Cc: Charlie Root <root@localhost>\r\n",
		"Subject: Ansible-report\r\n",
		"Content-Type: text/plain; charset=\"us-ascii\"\r\n",
		"Reply-To: john@example.com\r\n",
		"X-Special: Something or other\r\n",
		"\r\n\r\nHello there",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\nfull message:\n%s", want, msg)
		}
	}
	// bcc is never passed into mailBuildMessage at all — moduleMail only
	// ever hands it envelope recipients, never headers.
	if strings.Contains(msg, "Bcc") {
		t.Errorf("message must never contain a Bcc header: %s", msg)
	}
}

func TestMailBuildMessageOmitsEmptyOptionalHeaders(t *testing.T) {
	msg := mailBuildMessage("root", []string{"root"}, nil, "", "", "plain", "utf-8", nil, "ansible")
	if strings.Contains(msg, "Subject:") {
		t.Errorf("want no Subject header when subject is empty: %s", msg)
	}
	if strings.Contains(msg, "Cc:") {
		t.Errorf("want no Cc header when cc is empty: %s", msg)
	}
}

func TestModuleMailCurlMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v curl": {RC: 1},
	})
	res, err := moduleMail(context.Background(), conn, map[string]any{"subject": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: curl binary missing")
	}
}

func TestModuleMailDefaults(t *testing.T) {
	url := "smtp://localhost:25"
	fromFlag := "--mail-from " + shellQuote("<root>")
	rcptFlag := "--mail-rcpt " + shellQuote("<root>")
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v curl": {RC: 0},
	})
	res, err := moduleMail(context.Background(), conn, map[string]any{"subject": "System provisioned"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: mail sending is never idempotent")
	}
	var sendCmd, sendStdin string
	for i, c := range conn.Commands {
		if strings.HasPrefix(c, "curl ") {
			sendCmd, sendStdin = c, conn.Stdins[i]
		}
	}
	if sendCmd == "" {
		t.Fatal("want a curl command to have been run")
	}
	if !strings.Contains(sendCmd, "--url "+shellQuote(url)) {
		t.Errorf("cmd = %q, want url %q", sendCmd, url)
	}
	if !strings.Contains(sendCmd, fromFlag) {
		t.Errorf("cmd = %q, want %q", sendCmd, fromFlag)
	}
	if !strings.Contains(sendCmd, rcptFlag) {
		t.Errorf("cmd = %q, want %q", sendCmd, rcptFlag)
	}
	if !strings.Contains(sendStdin, "Subject: System provisioned\r\n") {
		t.Errorf("stdin = %q, want Subject header", sendStdin)
	}
}

func TestModuleMailBccInEnvelopeNotHeaders(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v curl": {RC: 0},
	})
	_, err := moduleMail(context.Background(), conn, map[string]any{
		"to":  []any{"John Doe <j.d@example.org>"},
		"bcc": []any{"Secret <secret@example.org>"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sendCmd, sendStdin string
	for i, c := range conn.Commands {
		if strings.HasPrefix(c, "curl ") {
			sendCmd, sendStdin = c, conn.Stdins[i]
		}
	}
	if !strings.Contains(sendCmd, "--mail-rcpt "+shellQuote("<secret@example.org>")) {
		t.Errorf("cmd = %q, want bcc address in envelope --mail-rcpt", sendCmd)
	}
	if strings.Contains(sendStdin, "secret@example.org") {
		t.Errorf("stdin must never contain the bcc address: %s", sendStdin)
	}
}

func TestModuleMailSendFailure(t *testing.T) {
	sendCmd := mailCurlCmd("localhost", 25, "root", []string{"root"}, "", "", "try", 20)
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v curl": {RC: 0},
		sendCmd:           {RC: 67, Stderr: "curl: (7) Failed to connect to localhost port 25: Connection refused"},
	})
	res, err := moduleMail(context.Background(), conn, map[string]any{"subject": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when curl exits non-zero")
	}
	if res.Msg == "" || !strings.Contains(res.Msg, "Connection refused") {
		t.Fatalf("want an explanatory message including stderr, got %q", res.Msg)
	}
}

func TestModuleMailValidation(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v curl": {RC: 0},
	})
	if _, err := moduleMail(context.Background(), conn, map[string]any{"subtype": "bogus"}); err == nil {
		t.Fatal("want error for invalid subtype")
	}
	if _, err := moduleMail(context.Background(), conn, map[string]any{"secure": "bogus"}); err == nil {
		t.Fatal("want error for invalid secure")
	}
}
