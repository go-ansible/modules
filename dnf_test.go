package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDnfInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
		"dnf install -y curl":         {RC: 0},
	})
	res, err := moduleDnf(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnfAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleDnf(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleDnfAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 0},
		"dnf remove -y curl":          {RC: 0},
	})
	res, err := moduleDnf(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnfAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleDnf(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleDnfLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf update -y curl": {RC: 0},
	})
	res, err := moduleDnf(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnfMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDnf(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleDnfNameList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
		"rpm -q git >/dev/null 2>&1":  {RC: 1},
		"dnf install -y curl git":     {RC: 0},
	})
	res, err := moduleDnf(context.Background(), conn, map[string]any{"name": []any{"curl", "git"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnf5Install(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
		"dnf5 install -y curl":        {RC: 0},
	})
	res, err := moduleDnf5(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnf5AlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleDnf5(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleDnf5MissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDnf5(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
