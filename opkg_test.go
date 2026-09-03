package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOpkgInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"opkg list-installed": {RC: 0, Stdout: ""},
		"opkg install foo":    {RC: 0},
	})
	res, err := moduleOpkg(context.Background(), conn, map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOpkgAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"opkg list-installed": {RC: 0, Stdout: "foo - 1.2-r1"},
	})
	res, err := moduleOpkg(context.Background(), conn, map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleOpkgAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"opkg list-installed": {RC: 0, Stdout: "foo - 1.2-r1"},
		"opkg remove foo":     {RC: 0},
	})
	res, err := moduleOpkg(context.Background(), conn, map[string]any{
		"name": "foo", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOpkgUpdateCacheAndForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"opkg update":                        {RC: 0},
		"opkg list-installed":                {RC: 0, Stdout: ""},
		"opkg install --force-overwrite foo": {RC: 0},
	})
	res, err := moduleOpkg(context.Background(), conn, map[string]any{
		"name": "foo", "update_cache": true, "force": "overwrite",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 3 || conn.Commands[0] != "opkg update" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleOpkgLatestUnsupported(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"opkg list-installed": {RC: 0, Stdout: ""},
	})
	_, err := moduleOpkg(context.Background(), conn, map[string]any{
		"name": "foo", "state": "latest",
	})
	if err == nil {
		t.Fatal("want error: opkg has no latest state")
	}
}

func TestModuleOpkgPkgAlias(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"opkg list-installed": {RC: 0, Stdout: ""},
		"opkg install foo":    {RC: 0},
	})
	res, err := moduleOpkg(context.Background(), conn, map[string]any{"pkg": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOpkgMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOpkg(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
