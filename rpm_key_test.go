package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRpmKeyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm --import " + shellQuote("http://example.com/key.gpg"): {RC: 0},
	})
	res, err := moduleRpmKey(context.Background(), conn, map[string]any{"key": "http://example.com/key.gpg"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRpmKeyAbsentFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -qa 'gpg-pubkey-*' 2>/dev/null":                   {RC: 0, Stdout: "gpg-pubkey-abc12345-def67890\n"},
		"rpm -e " + shellQuote("gpg-pubkey-abc12345-def67890"): {RC: 0},
	})
	res, err := moduleRpmKey(context.Background(), conn, map[string]any{"key": "abc12345", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRpmKeyAbsentNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -qa 'gpg-pubkey-*' 2>/dev/null": {RC: 0, Stdout: "gpg-pubkey-zzz00000-aaa11111\n"},
	})
	res, err := moduleRpmKey(context.Background(), conn, map[string]any{"key": "abc12345", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRpmKeyValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRpmKey(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing key")
	}
	if _, err := moduleRpmKey(context.Background(), conn, map[string]any{"key": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
