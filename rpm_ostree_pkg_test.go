package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRpmOstreePkgInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree install --allow-inactive --idempotent --unchanged-exit-77 nfs-utils": {RC: 0, Stdout: "Staged\n"},
	})
	res, err := moduleRpmOstreePkg(context.Background(), conn, map[string]any{"name": "nfs-utils"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRpmOstreePkgUnchanged77(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree install --allow-inactive --idempotent --unchanged-exit-77 nfs-utils": {RC: 77, Stdout: "Already installed\n"},
	})
	res, err := moduleRpmOstreePkg(context.Background(), conn, map[string]any{"name": "nfs-utils"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged/not-failed, res = %+v", res)
	}
}

func TestModuleRpmOstreePkgRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree uninstall --allow-inactive --idempotent --unchanged-exit-77 nfs-utils": {RC: 0, Stdout: "Removed\n"},
	})
	res, err := moduleRpmOstreePkg(context.Background(), conn, map[string]any{"name": "nfs-utils", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRpmOstreePkgApplyLive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree install --apply-live --assumeyes --allow-inactive --idempotent --unchanged-exit-77 nfs-utils": {RC: 0},
	})
	res, err := moduleRpmOstreePkg(context.Background(), conn, map[string]any{
		"name": "nfs-utils", "apply_live": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRpmOstreePkgFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree install --allow-inactive --idempotent --unchanged-exit-77 nfs-utils": {RC: 1, Stderr: "error: transaction in progress"},
	})
	res, err := moduleRpmOstreePkg(context.Background(), conn, map[string]any{"name": "nfs-utils"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}

func TestModuleRpmOstreePkgMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRpmOstreePkg(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
