package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleYarnNoNameInstallsFromPackageJSON(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn install": {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleYarnNoNameWithPathUsesCwdFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn install --cwd /app": {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"path": "/app"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYarnNoNameGlobalDoesNotAffectBareInstall(t *testing.T) {
	// The name=="" branch composes "yarn install" directly and never
	// consults the `scope` (global) prefix at all — only the named
	// add/remove/list paths use it. So global=true with no name still
	// just runs a plain "yarn install".
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn install": {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"global": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "yarn install" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleYarnInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn list --pattern lodash 2>/dev/null | grep -qF lodash@": {RC: 1},
		"yarn add lodash": {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"name": "lodash"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYarnAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn list --pattern lodash 2>/dev/null | grep -qF lodash@": {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"name": "lodash"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleYarnAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn list --pattern lodash 2>/dev/null | grep -qF lodash@": {RC: 0},
		"yarn remove lodash": {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"name": "lodash", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYarnAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn list --pattern lodash 2>/dev/null | grep -qF lodash@": {RC: 1},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"name": "lodash", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleYarnLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn add lodash@latest": {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"name": "lodash", "state": "latest"})
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

func TestModuleYarnVersionPinned(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn list --pattern lodash 2>/dev/null | grep -qF lodash@": {RC: 1},
		"yarn add lodash@4.17.21":                                   {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"name": "lodash", "version": "4.17.21"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYarnGlobalNamed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn global list --pattern lodash 2>/dev/null | grep -qF lodash@": {RC: 1},
		"yarn global add lodash": {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"name": "lodash", "global": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYarnProductionFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yarn list --pattern lodash 2>/dev/null | grep -qF lodash@": {RC: 1},
		"yarn add --production lodash":                              {RC: 0},
	})
	res, err := moduleYarn(context.Background(), conn, map[string]any{"name": "lodash", "production": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
