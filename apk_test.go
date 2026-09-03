package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleApkInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apk info -e curl >/dev/null 2>&1": {RC: 1},
		"apk add -q curl":                  {RC: 0},
	})
	res, err := moduleApk(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleApkAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apk info -e curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleApk(context.Background(), conn, map[string]any{"name": "curl"})
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

func TestModuleApkAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apk info -e curl >/dev/null 2>&1": {RC: 0},
		"apk del -q curl":                  {RC: 0},
	})
	res, err := moduleApk(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleApkAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apk info -e curl >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleApk(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleApkLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apk add -q -u curl": {RC: 0},
	})
	res, err := moduleApk(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "apk add -q -u curl" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleApkUpdateCache(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apk update -q":                    {RC: 0},
		"apk info -e curl >/dev/null 2>&1": {RC: 1},
		"apk add -q curl":                  {RC: 0},
	})
	res, err := moduleApk(context.Background(), conn, map[string]any{"name": "curl", "update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 3 || conn.Commands[0] != "apk update -q" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleApkNameList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apk info -e curl >/dev/null 2>&1": {RC: 1},
		"apk info -e git >/dev/null 2>&1":  {RC: 1},
		"apk add -q curl git":              {RC: 0},
	})
	res, err := moduleApk(context.Background(), conn, map[string]any{"name": []any{"curl", "git"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleApkMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleApk(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
