package modules

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The config curlDo writes is a sequence of curl directives, one per
// PHYSICAL line. Every test here is about one question: can a VALUE
// become a DIRECTIVE?

// TestANewlineInABodyStaysInsideItsDirective holds the escape itself.
// A JSON body is legitimately full of newlines when it was
// pretty-printed, so the fix cannot be "refuse them" -- it has to be
// "carry them as data", which is what curl's own `\n` escape inside a
// quoted parameter is for.
func TestANewlineInABodyStaysInsideItsDirective(t *testing.T) {
	conn := &curlConn{body: "ok\nHTTPSTATUS:200"}
	_, _, err := curlDo(context.Background(), conn, curlReq{
		Method: "POST",
		URL:    "https://api.example.com/v1/thing",
		JSON:   "{\n  \"url\": \"https://elsewhere.example\"\n}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(conn.Stdins) != 1 {
		t.Fatalf("stdins = %d, want the one config curlDo writes", len(conn.Stdins))
	}
	cfg := conn.Stdins[0]

	// The whole point: three directives, three lines, however many
	// newlines the body had.
	lines := strings.Split(strings.TrimSuffix(cfg, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("config has %d lines, want 3 (url, request, data):\n%s", len(lines), cfg)
	}
	data := lines[2]
	if !strings.HasPrefix(data, `data = "`) {
		t.Fatalf("third directive is %q, want the data one", data)
	}
	// Control: the body is actually in there. Without this the test
	// would pass just as well on a curlDo that sent no body at all.
	if !strings.Contains(data, `\n`) || !strings.Contains(data, "elsewhere.example") {
		t.Errorf("body neither escaped nor present: %q", data)
	}
}

// TestCurlRefusesAControlCharacterInTheURL is the second lock. The
// escape already makes the newline harmless; this makes a URL that
// carries one a REFUSAL, because no legitimate caller has one.
func TestCurlRefusesAControlCharacterInTheURL(t *testing.T) {
	conn := &curlConn{body: "ok\nHTTPSTATUS:200"}
	_, _, err := curlDo(context.Background(), conn, curlReq{
		URL: "https://api.example.com/v1/thing\nurl = file:///etc/passwd",
	})
	if err == nil {
		t.Fatal("a URL carrying a newline was accepted")
	}
	if !strings.Contains(err.Error(), "control character") {
		t.Errorf("error = %v, want it to name the problem", err)
	}
	// Refused BEFORE anything ran: a check that lets the request go out
	// and complains afterwards has not refused anything.
	if len(conn.Commands) != 0 {
		t.Errorf("curl ran anyway: %q", conn.Commands)
	}

	// Positive control: the same request without the newline goes out.
	// Otherwise this test would pass against a curlDo that refuses
	// everything.
	ok := &curlConn{body: "ok\nHTTPSTATUS:200"}
	if _, _, err := curlDo(context.Background(), ok, curlReq{
		URL: "https://api.example.com/v1/thing",
	}); err != nil {
		t.Fatalf("the clean URL was refused too: %v", err)
	}
	if len(ok.Commands) != 1 {
		t.Errorf("commands = %q, want the one curl invocation", ok.Commands)
	}
}

func TestCurlRefusesAControlCharacterInAHeader(t *testing.T) {
	conn := &curlConn{body: "ok\nHTTPSTATUS:200"}
	_, _, err := curlDo(context.Background(), conn, curlReq{
		URL:     "https://api.example.com/",
		Headers: [][2]string{{"Authorization", "Bearer x\nupload-file = /etc/shadow"}},
	})
	if err == nil {
		t.Fatal("a header value carrying a newline was accepted")
	}
	if !strings.Contains(err.Error(), "Authorization") {
		t.Errorf("error = %v, want it to name the header", err)
	}
	if len(conn.Commands) != 0 {
		t.Errorf("curl ran anyway: %q", conn.Commands)
	}
}

// TestRealCurlDoesNotActOnAnInjectedDirective is the oracle: not what
// this package believes about curl's config format, but what curl 8
// does with the two configs -- the one the old escape produced and the
// one the new escape produces.
//
// It carries its own neuter. The first half reproduces the defect: the
// unescaped config makes curl perform a SECOND transfer, of a file the
// caller never named. If that half ever stops leaking, the second half
// proves nothing and the test says so rather than passing.
func TestRealCurlDoesNotActOnAnInjectedDirective(t *testing.T) {
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		// Not a skip: curl is how every HTTP module in this package
		// reaches a service, and a lane without it would quietly stop
		// testing the one thing this file is about.
		t.Fatalf("curl is required to run this test, and to run these modules at all: %v", err)
	}

	dir := t.TempDir()
	public := filepath.Join(dir, "public.txt")
	secret := filepath.Join(dir, "secret.txt")
	const sentinel = "SENTINEL-not-meant-to-be-fetched"
	if err := os.WriteFile(public, []byte("public\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte(sentinel+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// What an attacker-controlled URL looks like: a value that ends the
	// directive and starts another. The trailing `#` is why this is a
	// real attack rather than a near miss -- it makes the closing quote
	// this package writes land inside a COMMENT line, so the injected
	// directive is clean. Without it curl appends that quote to the
	// injected path and the fetch fails for the wrong reason, which is
	// what a first attempt at this test measured.
	hostile := "file://" + public + "\nurl = file://" + secret + "\n#"

	run := func(cfg string) string {
		t.Helper()
		cmd := exec.Command(curlPath, "-s", "-K", "-")
		cmd.Stdin = strings.NewReader(cfg)
		out, _ := cmd.Output() // curl exits non-zero on the rejected URL; the OUTPUT is the evidence
		return string(out)
	}

	// Neuter: the escape as it was, backslash and quote only.
	old := strings.ReplaceAll(hostile, `\`, `\\`)
	old = strings.ReplaceAll(old, `"`, `\"`)
	leaked := run("url = \"" + old + "\"\n")
	if !strings.Contains(leaked, sentinel) {
		t.Fatalf("the defect no longer reproduces, so this test proves nothing about the fix.\n"+
			"config was:\n%s\noutput was: %q", "url = \""+old+"\"\n", leaked)
	}

	// The fix.
	fetched := run("url = \"" + curlConfigEscape(hostile) + "\"\n")
	if strings.Contains(fetched, sentinel) {
		t.Errorf("curl still fetched the injected URL: %q", fetched)
	}
}

// FuzzCurlConfigEscape holds the one property the whole fix rests on:
// whatever goes in, what comes out is ONE line, and the quote this
// package writes around it is the only unescaped quote in the directive.
// Everything above tests the cases that were thought of; this tests the
// ones that were not.
func FuzzCurlConfigEscape(f *testing.F) {
	for _, seed := range []string{
		"",
		"https://api.online.net/api/v1/server/42",
		"a\nurl = file:///etc/passwd\n#",
		`a"b\c`,
		"tab\there",
		"\v\r\n",
		"\x00\x1b\x7f",
		"héllo",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		esc := curlConfigEscape(s)
		if strings.ContainsAny(esc, "\n\r") {
			t.Fatalf("curlConfigEscape(%q) = %q, which spans more than one directive line", s, esc)
		}
		// A quote that is not escaped would close the directive early,
		// and everything after it would be parsed as curl's own
		// arguments. Count backslashes before each quote: an odd run
		// escapes it.
		for i := range esc {
			if esc[i] != '"' {
				continue
			}
			back := 0
			for j := i - 1; j >= 0 && esc[j] == '\\'; j-- {
				back++
			}
			if back%2 == 0 {
				t.Fatalf("curlConfigEscape(%q) = %q has an unescaped quote at %d", s, esc, i)
			}
		}
	})
}

func TestCurlConfigSafeAcceptsWhatAURLIsMadeOf(t *testing.T) {
	for _, s := range []string{
		"https://api.online.net/api/v1/server/42",
		"https://x.example/a b", // a space is wrong in a URL, but it is not an injection
		"héllo",                 // multi-byte, and none of its bytes are C0
	} {
		if !curlConfigSafe(s) {
			t.Errorf("curlConfigSafe(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"a\nb", "a\rb", "a\tb", "a\vb", "a\x00b", "a\x1bb", "a\x7fb"} {
		if curlConfigSafe(s) {
			t.Errorf("curlConfigSafe(%q) = true, want false", s)
		}
	}
}
