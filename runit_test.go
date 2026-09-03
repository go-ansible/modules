package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRunitStartAlreadyRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sv status /var/service/dnscache": {RC: 0, Stdout: "run: /var/service/dnscache: (pid 123) 45s\n"},
	})
	res, err := moduleRunit(context.Background(), conn, map[string]any{"name": "dnscache", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when already running")
	}
	for _, c := range conn.Commands {
		if c == "sv start /var/service/dnscache" {
			t.Fatal("should not have started an already-running service")
		}
	}
}

func TestModuleRunitStartNotRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sv status /var/service/dnscache": {RC: 0, Stdout: "down: /var/service/dnscache: 45s\n"},
		"sv start /var/service/dnscache":  {RC: 0},
	})
	res, err := moduleRunit(context.Background(), conn, map[string]any{"name": "dnscache", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRunitRestartAlwaysChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sv restart /var/service/dnscache": {RC: 0},
	})
	res, err := moduleRunit(context.Background(), conn, map[string]any{"name": "dnscache", "state": "restarted"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRunitEnabledCreatesSymlink(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /var/service/dnscache":                {RC: 1},
		"ln -s /etc/sv/dnscache /var/service/dnscache": {RC: 0},
	})
	res, err := moduleRunit(context.Background(), conn, map[string]any{"name": "dnscache", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRunitDisabledStopsAndRemoves(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /var/service/dnscache":   {RC: 0},
		"sv status /var/service/dnscache": {RC: 0, Stdout: "run: /var/service/dnscache: (pid 123) 45s\n"},
		"sv stop /var/service/dnscache":   {RC: 0},
		"rm -f /var/service/dnscache":     {RC: 0},
	})
	res, err := moduleRunit(context.Background(), conn, map[string]any{"name": "dnscache", "enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRunitUnknownState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRunit(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleRunitMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRunit(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleRunitCustomServiceDir(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sv status /run/service/dnscache": {RC: 0, Stdout: "run: /run/service/dnscache: (pid 1) 1s\n"},
	})
	res, err := moduleRunit(context.Background(), conn, map[string]any{
		"name": "dnscache", "state": "started", "service_dir": "/run/service",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}
