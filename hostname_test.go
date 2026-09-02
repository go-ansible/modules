package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestHostnameSetCmd(t *testing.T) {
	cmd := hostnameSetCmd("web01")
	want := "if command -v hostnamectl >/dev/null 2>&1; then hostnamectl set-hostname web01; else hostname web01; fi"
	if cmd != want {
		t.Fatalf("cmd = %q, want %q", cmd, want)
	}
}

func TestModuleHostnameAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"hostname": {RC: 0, Stdout: "web01\n"},
	})
	res, err := moduleHostname(context.Background(), conn, map[string]any{"name": "web01"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no set command issued, commands = %v", conn.Commands)
	}
}

func TestModuleHostnameChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"hostname":            {RC: 0, Stdout: "old\n"},
		hostnameSetCmd("new"): {RC: 0},
	})
	res, err := moduleHostname(context.Background(), conn, map[string]any{"name": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Facts["ansible_hostname"] != "new" {
		t.Fatalf("facts = %v", res.Facts)
	}
	if res.Extra["name"] != "new" {
		t.Fatalf("extra = %v", res.Extra)
	}
}

func TestModuleHostnameMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHostname(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleHostnameProbeError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"hostname": {RC: 1, Stderr: "boom"},
	})
	if _, err := moduleHostname(context.Background(), conn, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error when probing the current hostname fails")
	}
}

func TestModuleHostnameSetError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"hostname":            {RC: 0, Stdout: "old\n"},
		hostnameSetCmd("new"): {RC: 1, Stderr: "permission denied"},
	})
	if _, err := moduleHostname(context.Background(), conn, map[string]any{"name": "new"}); err == nil {
		t.Fatal("want error when setting the hostname fails")
	}
}
