package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePkgngInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg update":                       {RC: 0},
		"pkg info -e curl >/dev/null 2>&1": {RC: 1},
		"pkg install -y curl":              {RC: 0},
	})
	res, err := modulePkgng(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePkgngInstallCached(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg info -e curl >/dev/null 2>&1": {RC: 1},
		"pkg install -y curl":              {RC: 0},
	})
	res, err := modulePkgng(context.Background(), conn, map[string]any{"name": "curl", "cached": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v, want no pkg update when cached", conn.Commands)
	}
}

func TestModulePkgngAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg update":                       {RC: 0},
		"pkg info -e curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePkgng(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePkgngAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg info -e curl >/dev/null 2>&1": {RC: 0},
		"pkg delete -y curl":               {RC: 0},
	})
	res, err := modulePkgng(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	// state=absent must not trigger a "pkg update" refresh.
	for _, c := range conn.Commands {
		if c == "pkg update" {
			t.Fatal("want no pkg update for state=absent")
		}
	}
}

func TestModulePkgngLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg update":          {RC: 0},
		"pkg upgrade -y curl": {RC: 0},
	})
	res, err := modulePkgng(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkgngWildcardLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg update":     {RC: 0},
		"pkg upgrade -y": {RC: 0},
	})
	res, err := modulePkgng(context.Background(), conn, map[string]any{"name": "*", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkgngWildcardPresentIsNoop(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := modulePkgng(context.Background(), conn, map[string]any{"name": "*", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("commands = %v, want none run", conn.Commands)
	}
}

func TestModulePkgngInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePkgng(context.Background(), conn, map[string]any{"name": "curl", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModulePkgngMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePkgng(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
