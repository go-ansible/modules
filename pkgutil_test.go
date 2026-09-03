package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePkgutilInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWcommon":  {RC: 1},
		"pkgutil -iy CSWcommon": {RC: 0},
	})
	res, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "CSWcommon", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePkgutilAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWcommon": {RC: 0},
	})
	res, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "CSWcommon", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePkgutilInstallFromSiteWithForceAndCatalog(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWnrpe": {RC: 1},
		"pkgutil -iy -U -t ftp://myinternal.repo/opencsw/kiel -f CSWnrpe": {RC: 0},
	})
	res, err := modulePkgutil(context.Background(), conn, map[string]any{
		"name": "CSWnrpe", "state": "present", "site": "ftp://myinternal.repo/opencsw/kiel",
		"update_catalog": true, "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkgutilAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWtop":  {RC: 0},
		"pkgutil -ry CSWtop": {RC: 0},
	})
	res, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "CSWtop", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkgutilAbsentIgnoresNonCSW(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "notcsw", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: non-CSW name is never considered pkgutil-installed")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("commands = %v, want no pkginfo probe for a non-CSW name", conn.Commands)
	}
}

func TestModulePkgutilWildcardPresentRejected(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "*", "state": "present"}); err == nil {
		t.Fatal("want error: state=present with name='*' is rejected")
	}
}

func TestModulePkgutilWildcardLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgutil -c":  {RC: 0, Stdout: "header line\nCSWfoo 1.0 2.0\n(more info)\n"},
		"pkgutil -uy": {RC: 0},
	})
	res, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "*", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePkgutilWildcardLatestAlreadyUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkgutil -c": {RC: 0, Stdout: "header line\nCSWfoo SAME 2.0\n(more info)\n"},
	})
	res, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "*", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePkgutilNamedLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWfoo":  {RC: 0},
		"pkgutil -c CSWfoo":  {RC: 0, Stdout: "header line\nCSWfoo 1.0 2.0\n(more info)\n"},
		"pkgutil -uy CSWfoo": {RC: 0},
	})
	res, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "CSWfoo", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkgutilMissingState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePkgutil(context.Background(), conn, map[string]any{"name": "CSWfoo"}); err == nil {
		t.Fatal("want error for missing state")
	}
}

func TestModulePkgutilMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePkgutil(context.Background(), conn, map[string]any{"state": "present"}); err == nil {
		t.Fatal("want error for missing name")
	}
}
