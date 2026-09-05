package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleBowerNoNameInstalls(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bower install": {RC: 0},
	})
	res, err := moduleBower(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleBowerNoNameLatestUpdates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bower update": {RC: 0},
	})
	res, err := moduleBower(context.Background(), conn, map[string]any{"state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBowerInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`bower list --json 2>/dev/null | grep -q '"lodash"'`: {RC: 1},
		"bower install lodash":                               {RC: 0},
	})
	res, err := moduleBower(context.Background(), conn, map[string]any{"name": "lodash"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBowerAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`bower list --json 2>/dev/null | grep -q '"lodash"'`: {RC: 0},
	})
	res, err := moduleBower(context.Background(), conn, map[string]any{"name": "lodash"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleBowerAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`bower list --json 2>/dev/null | grep -q '"lodash"'`: {RC: 1},
	})
	res, err := moduleBower(context.Background(), conn, map[string]any{"name": "lodash", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleBowerAbsentRemoves(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`bower list --json 2>/dev/null | grep -q '"lodash"'`: {RC: 0},
		"bower uninstall lodash":                             {RC: 0},
	})
	res, err := moduleBower(context.Background(), conn, map[string]any{"name": "lodash", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBowerVersionPinned(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`bower list --json 2>/dev/null | grep -q '"lodash"'`: {RC: 1},
		"bower install lodash#4.17.21":                       {RC: 0},
	})
	res, err := moduleBower(context.Background(), conn, map[string]any{"name": "lodash", "version": "4.17.21"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBowerPathAndOffline(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`bower list --config.cwd=/app --json 2>/dev/null | grep -q '"lodash"'`: {RC: 1},
		"bower install --config.cwd=/app --offline lodash":                     {RC: 0},
	})
	res, err := moduleBower(context.Background(), conn, map[string]any{"name": "lodash", "path": "/app", "offline": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
