package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNpmNoNameInstallsFromPackageJSON(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm install": {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleNpmNoNameCI(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm ci": {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"ci": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNpmNoNameLatestUpdates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm update": {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNpmNoNameWithPathChdirs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cd /app && npm install --prefix /app": {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"path": "/app"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNpmNoNameGlobalIgnoresPath(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm install -g": {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"global": true, "path": "/app"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "npm install -g" {
		t.Fatalf("commands = %v, want global install without cd/prefix", conn.Commands)
	}
}

func TestModuleNpmInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm list lodash >/dev/null 2>&1": {RC: 1},
		"npm install lodash":              {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"name": "lodash"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNpmAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm list lodash >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"name": "lodash"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleNpmAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm list lodash >/dev/null 2>&1": {RC: 0},
		"npm uninstall lodash":            {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"name": "lodash", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNpmAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm list lodash >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"name": "lodash", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleNpmLatestNamed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm install lodash@latest": {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"name": "lodash", "state": "latest"})
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

func TestModuleNpmVersionPinned(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm list lodash >/dev/null 2>&1": {RC: 1},
		"npm install lodash@4.17.21":      {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"name": "lodash", "version": "4.17.21"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNpmGlobalNamed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm list -g lodash >/dev/null 2>&1": {RC: 1},
		"npm install -g lodash":              {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"name": "lodash", "global": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNpmProductionFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"npm list lodash >/dev/null 2>&1": {RC: 1},
		"npm install --production lodash": {RC: 0},
	})
	res, err := moduleNpm(context.Background(), conn, map[string]any{"name": "lodash", "production": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
