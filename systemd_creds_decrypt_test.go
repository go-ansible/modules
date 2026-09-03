package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSystemdCredsDecrypt(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds decrypt --name=db - -": {RC: 0, Stdout: "access_token"},
	})
	res, err := moduleSystemdCredsDecrypt(context.Background(), conn, map[string]any{
		"name": "db", "secret": "WhQZht+JQJax1aZemmGLxmAAAA...",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["value"] != "access_token" {
		t.Fatalf("value = %v", res.Extra["value"])
	}
	if conn.Stdins[0] != "WhQZht+JQJax1aZemmGLxmAAAA..." {
		t.Fatalf("stdins = %v", conn.Stdins)
	}
}

func TestModuleSystemdCredsDecryptTranscode(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds decrypt --transcode=base64 - -": {RC: 0, Stdout: "YWNjZXNzX3Rva2Vu"},
	})
	res, err := moduleSystemdCredsDecrypt(context.Background(), conn, map[string]any{
		"secret": "x", "transcode": "base64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != "YWNjZXNzX3Rva2Vu" {
		t.Fatalf("value = %v", res.Extra["value"])
	}
}

func TestModuleSystemdCredsDecryptFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds decrypt - -": {RC: 1, Stderr: "bad ciphertext"},
	})
	res, err := moduleSystemdCredsDecrypt(context.Background(), conn, map[string]any{"secret": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for non-zero exit")
	}
}

func TestModuleSystemdCredsDecryptMissingSecret(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSystemdCredsDecrypt(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing secret")
	}
}
