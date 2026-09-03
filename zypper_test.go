package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleZypperInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
		"zypper --non-interactive --quiet install --type package --auto-agree-with-licenses --no-recommends -- curl": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleZypperAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleZypperAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1":                                    {RC: 0},
		"zypper --non-interactive --quiet remove --type package -- curl": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZypperDisableGPGCheck(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
		"zypper --non-interactive --quiet --no-gpg-checks install --type package --auto-agree-with-licenses --no-recommends -- curl": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl", "disable_gpg_check": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZypperDisableRecommendsFalse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
		"zypper --non-interactive --quiet install --type package --auto-agree-with-licenses -- curl": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl", "disable_recommends": false})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZypperUpdateCache(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --non-interactive --quiet refresh": {RC: 0},
		"rpm -q curl >/dev/null 2>&1":              {RC: 1},
		"zypper --non-interactive --quiet install --type package --auto-agree-with-licenses --no-recommends -- curl": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl", "update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != "zypper --non-interactive --quiet refresh" {
		t.Fatalf("commands = %v, want refresh first", conn.Commands)
	}
}

func TestModuleZypperTransactionalUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
		"transactional-update --continue --drop-if-no-change --quiet run zypper --non-interactive --quiet install --type package --auto-agree-with-licenses --no-recommends -- curl": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl", "transactional_update": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZypperLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --non-interactive --quiet update --type package --auto-agree-with-licenses --no-recommends -- curl": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZypperDistUpgrade(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --non-interactive --quiet dist-upgrade --auto-agree-with-licenses --no-recommends": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"state": "dist-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZypperNonPackageTypeAlwaysActs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --non-interactive --quiet install --type pattern --auto-agree-with-licenses --no-recommends -- devel_C_C++": {RC: 0},
	})
	res, err := moduleZypper(context.Background(), conn, map[string]any{"name": "devel_C_C++", "type": "pattern"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: non-package types are never treated as already-satisfied")
	}
}

func TestModuleZypperNoNamesIsNoop(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleZypper(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleZypperInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZypper(context.Background(), conn, map[string]any{"name": "curl", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}
