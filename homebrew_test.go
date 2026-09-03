package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHomebrewInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew list --formula curl >/dev/null 2>&1": {RC: 1},
		"brew install curl":                        {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleHomebrewAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew list --formula curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHomebrewAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew list --formula curl >/dev/null 2>&1": {RC: 0},
		"brew uninstall curl":                      {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew list --formula curl >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHomebrewLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew upgrade curl": {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewUpgradedAlias(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew upgrade curl": {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl", "state": "upgraded"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewUpdateHomebrew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew update": {RC: 0},
		"brew list --formula curl >/dev/null 2>&1": {RC: 1},
		"brew install curl":                        {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl", "update_homebrew": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != "brew update" {
		t.Fatalf("commands = %v, want brew update first", conn.Commands)
	}
}

func TestModuleHomebrewHead(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew install --HEAD curl": {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl", "state": "head"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no query for head", conn.Commands)
	}
}

func TestModuleHomebrewLinked(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew link curl": {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl", "state": "linked"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewUnlinked(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew unlink curl": {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": "curl", "state": "unlinked"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrew(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleHomebrewNameList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew list --formula curl >/dev/null 2>&1": {RC: 1},
		"brew list --formula git >/dev/null 2>&1":  {RC: 1},
		"brew install curl git":                    {RC: 0},
	})
	res, err := moduleHomebrew(context.Background(), conn, map[string]any{"name": []any{"curl", "git"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
