package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHomebrewServicesPresentAlreadyStarted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew services list 2>/dev/null | grep -qE " + shellQuote("^curl +started"): {RC: 0},
	})
	res, err := moduleHomebrewServices(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no start attempted, commands = %v", conn.Commands)
	}
}

func TestModuleHomebrewServicesPresentStarts(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew services list 2>/dev/null | grep -qE " + shellQuote("^curl +started"): {RC: 1},
		"brew services start curl": {RC: 0},
	})
	res, err := moduleHomebrewServices(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewServicesAbsentAlreadyStopped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew services list 2>/dev/null | grep -qE " + shellQuote("^curl +started"): {RC: 1},
	})
	res, err := moduleHomebrewServices(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHomebrewServicesAbsentStops(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew services list 2>/dev/null | grep -qE " + shellQuote("^curl +started"): {RC: 0},
		"brew services stop curl": {RC: 0},
	})
	res, err := moduleHomebrewServices(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewServicesRestarted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew services restart curl": {RC: 0},
	})
	res, err := moduleHomebrewServices(context.Background(), conn, map[string]any{"name": "curl", "state": "restarted"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no check for restarted (always an action)", conn.Commands)
	}
}

func TestModuleHomebrewServicesInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewServices(context.Background(), conn, map[string]any{"name": "curl", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleHomebrewServicesMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewServices(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
