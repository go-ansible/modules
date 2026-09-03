package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOpenbsdPkgInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg_info -e nmap >/dev/null 2>&1": {RC: 1},
		"pkg_add -Im nmap":                 {RC: 0},
	})
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{"name": "nmap"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleOpenbsdPkgAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg_info -e nmap >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{"name": "nmap"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleOpenbsdPkgAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg_info -e mpd >/dev/null 2>&1": {RC: 0},
		"pkg_delete -Ic mpd":              {RC: 0},
	})
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{
		"name": "mpd", "state": "absent", "clean": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOpenbsdPkgAbsentQuick(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg_info -e qt5 >/dev/null 2>&1": {RC: 0},
		"pkg_delete -Iq qt5":              {RC: 0},
	})
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{
		"name": "qt5", "state": "absent", "quick": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOpenbsdPkgLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg_add -um nmap": {RC: 0},
	})
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{"name": "nmap", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOpenbsdPkgWildcardLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg_add -um": {RC: 0},
	})
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{"name": "*", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOpenbsdPkgWildcardPresentIsNoop(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{"name": "*"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("commands = %v, want none run", conn.Commands)
	}
}

func TestModuleOpenbsdPkgAutoremove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg_delete -Ia": {RC: 0},
	})
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{"name": "*", "autoremove": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "pkg_delete -Ia" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleOpenbsdPkgInstallWithAutoremove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg_info -e tree >/dev/null 2>&1": {RC: 1},
		"pkg_info -e mtr >/dev/null 2>&1":  {RC: 1},
		"pkg_add -Im tree mtr":             {RC: 0},
		"pkg_delete -Ia":                   {RC: 0},
	})
	res, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{
		"name": []any{"tree", "mtr"}, "autoremove": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOpenbsdPkgMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOpenbsdPkg(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
