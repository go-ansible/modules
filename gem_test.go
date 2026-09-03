package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGemInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gem list -i rails >/dev/null 2>&1": {RC: 1},
		"gem install --user-install rails":  {RC: 0},
	})
	res, err := moduleGem(context.Background(), conn, map[string]any{"name": "rails"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleGemAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gem list -i rails >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleGem(context.Background(), conn, map[string]any{"name": "rails"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no install attempted, commands = %v", conn.Commands)
	}
}

func TestModuleGemAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gem list -i rails >/dev/null 2>&1": {RC: 0},
		"gem uninstall rails":               {RC: 0},
	})
	res, err := moduleGem(context.Background(), conn, map[string]any{"name": "rails", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGemAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gem list -i rails >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleGem(context.Background(), conn, map[string]any{"name": "rails", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleGemLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gem update --user-install rails": {RC: 0},
	})
	res, err := moduleGem(context.Background(), conn, map[string]any{"name": "rails", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no query for latest", conn.Commands)
	}
}

func TestModuleGemVersionPinned(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gem list -i rails -v 7.0.0 >/dev/null 2>&1": {RC: 1},
		"gem install -v 7.0.0 --user-install rails":  {RC: 0},
	})
	res, err := moduleGem(context.Background(), conn, map[string]any{"name": "rails", "version": "7.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGemUserInstallFalse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gem list -i rails >/dev/null 2>&1":   {RC: 1},
		"gem install --no-user-install rails": {RC: 0},
	})
	res, err := moduleGem(context.Background(), conn, map[string]any{"name": "rails", "user_install": false})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGemCustomExecutable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/usr/bin/gem2.7 list -i rails >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleGem(context.Background(), conn, map[string]any{"name": "rails", "executable": "/usr/bin/gem2.7"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleGemMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGem(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
