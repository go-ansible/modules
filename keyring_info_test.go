package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeyringInfoFound(t *testing.T) {
	getCmd := keyringGetCmdForTest("svc", "user", "kpw")
	conn := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 0, Stdout: "Password123"},
	})
	res, err := moduleKeyringInfo(context.Background(), conn, map[string]any{
		"service": "svc", "username": "user", "keyring_password": "kpw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Extra["passphrase"] != "Password123" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeyringInfoNotFound(t *testing.T) {
	getCmd := keyringGetCmdForTest("svc", "user", "kpw")
	conn := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 1},
	})
	res, err := moduleKeyringInfo(context.Background(), conn, map[string]any{
		"service": "svc", "username": "user", "keyring_password": "kpw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if _, ok := res.Extra["passphrase"]; ok {
		t.Fatalf("want no passphrase key when not found, got %+v", res.Extra)
	}
}

func TestModuleKeyringInfoMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeyringInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
