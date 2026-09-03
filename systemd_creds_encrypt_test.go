package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSystemdCredsEncrypt(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds encrypt --name=db --not-after=+48hr - -": {
			RC:     0,
			Stdout: "WhQZht+JQJax1aZemmGLxmAAAA...\n",
		},
	})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{
		"name": "db", "not_after": "+48hr", "secret": "access_token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["value"] != "WhQZht+JQJax1aZemmGLxmAAAA..." {
		t.Fatalf("value = %v", res.Extra["value"])
	}
	if len(conn.Stdins) == 0 || conn.Stdins[0] != "access_token" {
		t.Fatalf("stdins = %v, secret must be sent via stdin, not argv", conn.Stdins)
	}
	for _, c := range conn.Commands {
		if strings.Contains(c, "access_token") {
			t.Fatalf("command %q leaks the secret in argv", c)
		}
	}
}

func TestModuleSystemdCredsEncryptPretty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds encrypt --pretty - -": {RC: 0, Stdout: "encrypted\n"},
	})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", "pretty": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != "encrypted" {
		t.Fatalf("value = %v", res.Extra["value"])
	}
}

func TestModuleSystemdCredsEncryptFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds encrypt - -": {RC: 1, Stderr: "systemd-creds requires systemd 250 or later"},
	})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for non-zero exit")
	}
}

func TestModuleSystemdCredsEncryptMissingSecret(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing secret")
	}
}
