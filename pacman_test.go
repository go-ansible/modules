package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacmanNameAndUpgradeMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePacman(context.Background(), conn, map[string]any{
		"name": "curl", "upgrade": true,
	}); err == nil {
		t.Fatal("want error: name and upgrade are mutually exclusive")
	}
}

func TestModulePacmanUpdateCacheOnlyNoName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -Sy --noconfirm": {RC: 0},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if res.Msg != "nothing to do" {
		t.Fatalf("msg = %q", res.Msg)
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "pacman -Sy --noconfirm" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePacmanNoArgsIsNoOp(t *testing.T) {
	// With neither name, upgrade, nor update_cache set, pacman.go treats
	// this as a no-op rather than an argument error (matching real
	// pacman's own tolerance of an update_cache-only invocation).
	conn := newFakeConn(nil)
	res, err := modulePacman(context.Background(), conn, map[string]any{})
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

func TestModulePacmanUpgrade(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -Su --noconfirm": {RC: 0},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"upgrade": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacmanUpgradeWithUpdateCache(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -Sy --noconfirm": {RC: 0},
		"pacman -Su --noconfirm": {RC: 0},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"upgrade": true, "update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 || conn.Commands[0] != "pacman -Sy --noconfirm" || conn.Commands[1] != "pacman -Su --noconfirm" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePacmanInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -Q curl >/dev/null 2>&1": {RC: 1},
		"pacman -S --noconfirm curl":     {RC: 0},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacmanAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -Q curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePacmanAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -Q curl >/dev/null 2>&1": {RC: 0},
		"pacman -R --noconfirm curl":     {RC: 0},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacmanAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -Q curl >/dev/null 2>&1": {RC: 1},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePacmanLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -S --noconfirm curl": {RC: 0},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
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

func TestModulePacmanNameList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman -Q curl >/dev/null 2>&1": {RC: 1},
		"pacman -Q git >/dev/null 2>&1":  {RC: 1},
		"pacman -S --noconfirm curl git": {RC: 0},
	})
	res, err := modulePacman(context.Background(), conn, map[string]any{"name": []any{"curl", "git"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
