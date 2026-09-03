package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePkginInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin list 2>/dev/null | grep -q '^foo-'": {RC: 1},
		"pkgin -y install foo":                     {RC: 0},
	})
	res, err := modulePkgin(context.Background(), conn, map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePkginAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin list 2>/dev/null | grep -q '^foo-'": {RC: 0},
	})
	res, err := modulePkgin(context.Background(), conn, map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePkginAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin list 2>/dev/null | grep -q '^foo-'": {RC: 0},
		"pkgin -y remove foo":                      {RC: 0},
	})
	res, err := modulePkgin(context.Background(), conn, map[string]any{"name": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkginUpdateCacheAndInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin -y update":                          {RC: 0},
		"pkgin list 2>/dev/null | grep -q '^foo-'": {RC: 1},
		"pkgin -y install foo":                     {RC: 0},
	})
	res, err := modulePkgin(context.Background(), conn, map[string]any{"name": "foo", "update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != "pkgin -y update" {
		t.Fatalf("commands = %v, want update first", conn.Commands)
	}
}

func TestModulePkginUpdateCacheOnly(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin -y update": {RC: 0},
	})
	res, err := modulePkgin(context.Background(), conn, map[string]any{"update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkginUpgrade(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin -y upgrade": {RC: 0},
	})
	res, err := modulePkgin(context.Background(), conn, map[string]any{"upgrade": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkginFullUpgradeWithForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin -y -F full-upgrade": {RC: 0},
	})
	res, err := modulePkgin(context.Background(), conn, map[string]any{"full_upgrade": true, "force": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkginClean(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin -y clean": {RC: 0},
	})
	res, err := modulePkgin(context.Background(), conn, map[string]any{"clean": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkginNothingRequired(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePkgin(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: at least one of name/update_cache/upgrade/full_upgrade/clean is required")
	}
}

func TestModulePkginLatestUnsupported(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgin list 2>/dev/null | grep -q '^foo-'": {RC: 1},
	})
	if _, err := modulePkgin(context.Background(), conn, map[string]any{"name": "foo", "state": "latest"}); err == nil {
		t.Fatal("want error: pkgin has no state=latest")
	}
}
