package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRhelRpmOstreeInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q htop >/dev/null 2>&1":    {RC: 1},
		"rpm -q ansible >/dev/null 2>&1": {RC: 0},
		"rpm-ostree install htop":        {RC: 0},
	})
	res, err := moduleRhelRpmOstree(context.Background(), conn, map[string]any{
		"name": []any{"htop", "ansible"}, "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRhelRpmOstreeAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q htop >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleRhelRpmOstree(context.Background(), conn, map[string]any{"name": "htop"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRhelRpmOstreeRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q htop >/dev/null 2>&1": {RC: 0},
		"rpm-ostree uninstall htop":   {RC: 0},
	})
	res, err := moduleRhelRpmOstree(context.Background(), conn, map[string]any{"name": "htop", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRhelRpmOstreeRemoveAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q htop >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleRhelRpmOstree(context.Background(), conn, map[string]any{"name": "htop", "state": "removed"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRhelRpmOstreeLatestAlwaysRuns(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree install htop": {RC: 0},
	})
	res, err := moduleRhelRpmOstree(context.Background(), conn, map[string]any{"name": "htop", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRhelRpmOstreeMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRhelRpmOstree(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleRhelRpmOstreeUnknownState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRhelRpmOstree(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}
