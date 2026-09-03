package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSlackpkgInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ls /var/log/packages 2>/dev/null | grep -q '^curl-'": {RC: 1},
		"slackpkg -default_answer=y -batch=on install curl":   {RC: 0},
	})
	res, err := moduleSlackpkg(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSlackpkgAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ls /var/log/packages 2>/dev/null | grep -q '^curl-'": {RC: 0},
	})
	res, err := moduleSlackpkg(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSlackpkgAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ls /var/log/packages 2>/dev/null | grep -q '^curl-'": {RC: 0},
		"slackpkg -default_answer=y -batch=on remove curl":    {RC: 0},
	})
	res, err := moduleSlackpkg(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSlackpkgLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"slackpkg -default_answer=y -batch=on upgrade curl": {RC: 0},
	})
	res, err := moduleSlackpkg(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSlackpkgUpdateCache(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"slackpkg -batch=on update":                           {RC: 0},
		"ls /var/log/packages 2>/dev/null | grep -q '^curl-'": {RC: 1},
		"slackpkg -default_answer=y -batch=on install curl":   {RC: 0},
	})
	res, err := moduleSlackpkg(context.Background(), conn, map[string]any{"name": "curl", "update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != "slackpkg -batch=on update" {
		t.Fatalf("commands = %v, want update first", conn.Commands)
	}
}

func TestModuleSlackpkgNameList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ls /var/log/packages 2>/dev/null | grep -q '^curl-'": {RC: 1},
		"ls /var/log/packages 2>/dev/null | grep -q '^git-'":  {RC: 1},
		"slackpkg -default_answer=y -batch=on install curl":   {RC: 0},
		"slackpkg -default_answer=y -batch=on install git":    {RC: 0},
	})
	res, err := moduleSlackpkg(context.Background(), conn, map[string]any{"name": []any{"curl", "git"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	// Real slackpkg installs each package with its own invocation.
	if len(conn.Commands) != 4 {
		t.Fatalf("commands = %v, want one slackpkg call per package", conn.Commands)
	}
}

func TestModuleSlackpkgMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSlackpkg(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
