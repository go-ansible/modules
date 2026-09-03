package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSvr4pkgInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWcommon":                    {RC: 1},
		"pkgadd -n -d /tmp/cswpkgs.pkg CSWcommon": {RC: 0},
	})
	res, err := moduleSvr4pkg(context.Background(), conn, map[string]any{
		"name": "CSWcommon", "state": "present", "src": "/tmp/cswpkgs.pkg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSvr4pkgInstallWithProxyAndZone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWpkgutil": {RC: 1},
		"pkgadd -n -G -d http://get.opencsw.org/now -x proxy.example.com CSWpkgutil": {RC: 0},
	})
	res, err := moduleSvr4pkg(context.Background(), conn, map[string]any{
		"name": "CSWpkgutil", "state": "present", "src": "http://get.opencsw.org/now",
		"zone": "current", "proxy": "proxy.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSvr4pkgAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWcommon": {RC: 0},
	})
	res, err := moduleSvr4pkg(context.Background(), conn, map[string]any{
		"name": "CSWcommon", "state": "present", "src": "/tmp/x.pkg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSvr4pkgMissingSrc(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q CSWcommon": {RC: 1},
	})
	if _, err := moduleSvr4pkg(context.Background(), conn, map[string]any{
		"name": "CSWcommon", "state": "present",
	}); err == nil {
		t.Fatal("want error: src required for state=present")
	}
}

func TestModuleSvr4pkgAbsentCategory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q -c FIREFOX": {RC: 0},
		"pkgrm -n -Y FIREFOX":   {RC: 0},
	})
	res, err := moduleSvr4pkg(context.Background(), conn, map[string]any{
		"name": "FIREFOX", "state": "absent", "category": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSvr4pkgAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkginfo -q SUNWgnome-sound-recorder": {RC: 1},
	})
	res, err := moduleSvr4pkg(context.Background(), conn, map[string]any{
		"name": "SUNWgnome-sound-recorder", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSvr4pkgMissingState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSvr4pkg(context.Background(), conn, map[string]any{"name": "foo"}); err == nil {
		t.Fatal("want error for missing state")
	}
}
