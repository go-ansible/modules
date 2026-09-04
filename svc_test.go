package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSvcStartAlreadyRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /service/dnscache":      {RC: 0},
		"test -e /service/dnscache/down": {RC: 1},
		"svstat /service/dnscache":       {RC: 0, Stdout: "/service/dnscache: up (pid 123) 45 seconds\n"},
	})
	res, err := moduleSvc(context.Background(), conn, map[string]any{"name": "dnscache", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	for _, c := range conn.Commands {
		if c == "svc -u /service/dnscache" {
			t.Fatal("should not have started an already-running service")
		}
	}
}

func TestModuleSvcStartNotRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /service/dnscache":      {RC: 0},
		"test -e /service/dnscache/down": {RC: 1},
		"svstat /service/dnscache":       {RC: 0, Stdout: "/service/dnscache: down 45 seconds\n"},
		"svc -u /service/dnscache":       {RC: 0},
	})
	res, err := moduleSvc(context.Background(), conn, map[string]any{"name": "dnscache", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSvcOnceAlwaysRuns(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /service/dnscache":      {RC: 0},
		"test -e /service/dnscache/down": {RC: 0},
		"svstat /service/dnscache":       {RC: 0, Stdout: "/service/dnscache: down 45 seconds, want up\n"},
		"svc -o /service/dnscache":       {RC: 0},
	})
	res, err := moduleSvc(context.Background(), conn, map[string]any{"name": "dnscache", "state": "once"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: once always runs")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "svc -o /service/dnscache" {
			found = true
		}
	}
	if !found {
		t.Fatal("want svc -o to have run, not a python-bug AttributeError-equivalent crash")
	}
}

func TestModuleSvcRestartAlwaysChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /service/dnscache":      {RC: 0},
		"test -e /service/dnscache/down": {RC: 1},
		"svstat /service/dnscache":       {RC: 0, Stdout: "/service/dnscache: up (pid 1) 1 seconds\n"},
		"svc -t /service/dnscache":       {RC: 0},
	})
	res, err := moduleSvc(context.Background(), conn, map[string]any{"name": "dnscache", "state": "restarted"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSvcEnableCreatesSymlink(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /service/dnscache":                     {RC: 1},
		"test -e /etc/service/dnscache/down":            {RC: 1},
		"test -e /etc/service/dnscache":                 {RC: 0},
		"ln -s /etc/service/dnscache /service/dnscache": {RC: 0},
	})
	res, err := moduleSvc(context.Background(), conn, map[string]any{"name": "dnscache", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSvcDisableStopsAndRemoves(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /service/dnscache":         {RC: 0},
		"test -e /service/dnscache/down":    {RC: 1},
		"svstat /service/dnscache":          {RC: 0, Stdout: "/service/dnscache: up (pid 1) 1 seconds\n"},
		"rm /service/dnscache":              {RC: 0},
		"svc -dx /etc/service/dnscache":     {RC: 0},
		"test -e /etc/service/dnscache/log": {RC: 1},
	})
	res, err := moduleSvc(context.Background(), conn, map[string]any{"name": "dnscache", "enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSvcUnknownState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSvc(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleSvcMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSvc(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
