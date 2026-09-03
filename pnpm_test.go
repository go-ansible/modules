package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePnpmNoNameInstallsFromPackageJSON(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm install": {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePnpmNoNameWithPathUsesDashCFlag(t *testing.T) {
	// Unlike npm.go, pnpm.go composes the directory via pnpm's own -C
	// flag rather than wrapping the command in "cd path && ...".
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm install -C /app": {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"path": "/app"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "pnpm install -C /app" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePnpmNoNameGlobal(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm install -g": {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"global": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePnpmInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm list lodash 2>/dev/null | grep -qF lodash": {RC: 1},
		"pnpm add lodash": {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"name": "lodash"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePnpmAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm list lodash 2>/dev/null | grep -qF lodash": {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"name": "lodash"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePnpmAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm list lodash 2>/dev/null | grep -qF lodash": {RC: 0},
		"pnpm remove lodash":                             {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"name": "lodash", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePnpmAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm list lodash 2>/dev/null | grep -qF lodash": {RC: 1},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"name": "lodash", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePnpmLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm add lodash@latest": {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"name": "lodash", "state": "latest"})
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

func TestModulePnpmVersionPinned(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm list lodash 2>/dev/null | grep -qF lodash": {RC: 1},
		"pnpm add lodash@4.17.21":                        {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"name": "lodash", "version": "4.17.21"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePnpmProductionFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pnpm list lodash 2>/dev/null | grep -qF lodash": {RC: 1},
		"pnpm add --prod lodash":                         {RC: 0},
	})
	res, err := modulePnpm(context.Background(), conn, map[string]any{"name": "lodash", "production": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
