package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleFilesystemCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /dev/sdb1":                            {RC: 0},
		"blkid -o value -s TYPE /dev/sdb1 2>/dev/null": {RC: 2},
		"mkfs.ext4 /dev/sdb1":                          {RC: 0},
	})
	res, err := moduleFilesystem(context.Background(), conn, map[string]any{
		"dev": "/dev/sdb1", "fstype": "ext4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleFilesystemAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /dev/sdb1":                            {RC: 0},
		"blkid -o value -s TYPE /dev/sdb1 2>/dev/null": {RC: 0, Stdout: "ext4\n"},
	})
	res, err := moduleFilesystem(context.Background(), conn, map[string]any{
		"dev": "/dev/sdb1", "fstype": "ext4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleFilesystemMismatchWithoutForceFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /dev/sdb1":                            {RC: 0},
		"blkid -o value -s TYPE /dev/sdb1 2>/dev/null": {RC: 0, Stdout: "xfs\n"},
	})
	res, err := moduleFilesystem(context.Background(), conn, map[string]any{
		"dev": "/dev/sdb1", "fstype": "ext4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed for mismatched fstype without force, res = %+v", res)
	}
}

func TestModuleFilesystemMismatchWithForceRecreates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /dev/sdb1":                            {RC: 0},
		"blkid -o value -s TYPE /dev/sdb1 2>/dev/null": {RC: 0, Stdout: "xfs\n"},
		"mkfs.ext4 -F /dev/sdb1":                       {RC: 0},
	})
	res, err := moduleFilesystem(context.Background(), conn, map[string]any{
		"dev": "/dev/sdb1", "fstype": "ext4", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleFilesystemResize(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /dev/sdb1":                            {RC: 0},
		"blkid -o value -s TYPE /dev/sdb1 2>/dev/null": {RC: 0, Stdout: "ext4\n"},
		"resize2fs /dev/sdb1":                          {RC: 0},
	})
	res, err := moduleFilesystem(context.Background(), conn, map[string]any{
		"dev": "/dev/sdb1", "fstype": "ext4", "resizefs": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleFilesystemAbsentWipes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /dev/sdb1":                            {RC: 0},
		"blkid -o value -s TYPE /dev/sdb1 2>/dev/null": {RC: 0, Stdout: "ext4\n"},
		"wipefs -a /dev/sdb1":                          {RC: 0},
	})
	res, err := moduleFilesystem(context.Background(), conn, map[string]any{
		"dev": "/dev/sdb1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleFilesystemAbsentMissingDevOk(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /dev/sdb1": {RC: 1},
	})
	res, err := moduleFilesystem(context.Background(), conn, map[string]any{
		"dev": "/dev/sdb1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleFilesystemMissingDev(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFilesystem(context.Background(), conn, map[string]any{"fstype": "ext4"}); err == nil {
		t.Fatal("want error for missing dev")
	}
}

func TestModuleFilesystemPresentMissingFstype(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /dev/sdb1": {RC: 0},
	})
	if _, err := moduleFilesystem(context.Background(), conn, map[string]any{"dev": "/dev/sdb1"}); err == nil {
		t.Fatal("want error for missing fstype")
	}
}
