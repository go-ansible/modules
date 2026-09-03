package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDpkgDivertCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg-divert --listpackage /usr/bin/busybox":                                               {RC: 0, Stdout: ""},
		"dpkg-divert --no-rename --local --divert /usr/bin/busybox.distrib --add /usr/bin/busybox": {RC: 0, Stdout: "Adding 'local diversion'\n"},
	})
	res, err := moduleDpkgDivert(context.Background(), conn, map[string]any{"path": "/usr/bin/busybox"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDpkgDivertCreateWithHolderAndRename(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg-divert --listpackage /usr/bin/busybox":                                                {RC: 0, Stdout: ""},
		"dpkg-divert --rename --local --divert /usr/bin/busybox.dpkg-divert --add /usr/bin/busybox": {RC: 0},
	})
	res, err := moduleDpkgDivert(context.Background(), conn, map[string]any{
		"path": "/usr/bin/busybox", "divert": "/usr/bin/busybox.dpkg-divert", "rename": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDpkgDivertAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg-divert --listpackage /usr/bin/busybox": {RC: 0, Stdout: "LOCAL\n"},
		"dpkg-divert --truename /usr/bin/busybox":    {RC: 0, Stdout: "/usr/bin/busybox.distrib\n"},
	})
	res, err := moduleDpkgDivert(context.Background(), conn, map[string]any{"path": "/usr/bin/busybox"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleDpkgDivertUpdateHolder(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg-divert --listpackage /usr/bin/busybox":                                                          {RC: 0, Stdout: "LOCAL\n"},
		"dpkg-divert --truename /usr/bin/busybox":                                                             {RC: 0, Stdout: "/usr/bin/busybox.distrib\n"},
		"dpkg-divert --no-rename --remove /usr/bin/busybox":                                                   {RC: 0},
		"dpkg-divert --no-rename --package branding --divert /usr/bin/busybox.distrib --add /usr/bin/busybox": {RC: 0},
	})
	res, err := moduleDpkgDivert(context.Background(), conn, map[string]any{
		"path": "/usr/bin/busybox", "holder": "branding",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDpkgDivertRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg-divert --listpackage /usr/bin/busybox":        {RC: 0, Stdout: "LOCAL\n"},
		"dpkg-divert --truename /usr/bin/busybox":           {RC: 0, Stdout: "/usr/bin/busybox.distrib\n"},
		"dpkg-divert --no-rename --remove /usr/bin/busybox": {RC: 0, Stdout: "Removing 'local diversion'\n"},
	})
	res, err := moduleDpkgDivert(context.Background(), conn, map[string]any{
		"path": "/usr/bin/busybox", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDpkgDivertRemoveAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg-divert --listpackage /usr/bin/busybox": {RC: 0, Stdout: ""},
	})
	res, err := moduleDpkgDivert(context.Background(), conn, map[string]any{
		"path": "/usr/bin/busybox", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleDpkgDivertMissingPath(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDpkgDivert(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
}
