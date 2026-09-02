package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const pkgMgrProbe = "if command -v apt-get >/dev/null 2>&1; then echo apt; " +
	"elif command -v dnf >/dev/null 2>&1; then echo dnf; " +
	"elif command -v yum >/dev/null 2>&1; then echo dnf; " +
	"else echo none; fi"

func TestModulePackageDelegatesToApt(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		pkgMgrProbe: {RC: 0, Stdout: "apt"},
		"dpkg -s curl 2>/dev/null | grep -q '^Status:.*installed'":  {RC: 1},
		"DEBIAN_FRONTEND=noninteractive apt-get install -y -q curl": {RC: 0},
	})
	res, err := modulePackage(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePackageDelegatesToDnf(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		pkgMgrProbe:                   {RC: 0, Stdout: "dnf"},
		"rpm -q curl >/dev/null 2>&1": {RC: 1},
		"dnf install -y curl":         {RC: 0},
	})
	res, err := modulePackage(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePackageNoManagerFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		pkgMgrProbe: {RC: 0, Stdout: "none"},
	})
	res, err := modulePackage(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: no package manager found")
	}
}

func TestDetectPackageManagerYumAlias(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		pkgMgrProbe: {RC: 0, Stdout: "dnf"},
	})
	mgr, err := detectPackageManager(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if mgr != "dnf" {
		t.Fatalf("mgr = %q, want dnf", mgr)
	}
}
