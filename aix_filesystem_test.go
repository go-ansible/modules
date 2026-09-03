package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAixFilesystemCreateOnExistingLV(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"mount | grep -qFw -- /testfs": {RC: 1},
		"lsfs -l /testfs":              {RC: 1, Stderr: "No record matching /testfs"},
		"crfs -v jfs2 -d testlv -m /testfs -A yes -t no -p rw -a agblksize=4096 -a isnapshot=no": {RC: 0, Stdout: "File system created."},
	})
	res, err := moduleAixFilesystem(context.Background(), conn, map[string]any{
		"device": "testlv", "filesystem": "/testfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixFilesystemAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"mount | grep -qFw -- /testfs": {RC: 1},
		"lsfs -l /testfs":              {RC: 0, Stdout: "/testfs -- testlv jfs2 ...\n"},
	})
	res, err := moduleAixFilesystem(context.Background(), conn, map[string]any{
		"filesystem": "/testfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixFilesystemUnmount(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"mount | grep -qFw -- /testfs": {RC: 0},
		"unmount /testfs":              {RC: 0},
	})
	res, err := moduleAixFilesystem(context.Background(), conn, map[string]any{
		"filesystem": "/testfs", "state": "unmounted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixFilesystemRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"mount | grep -qFw -- /newfs": {RC: 1},
		"lsfs -l /newfs":              {RC: 0, Stdout: "/newfs -- testlv jfs2 ...\n"},
		"rmfs -r /newfs":              {RC: 0},
	})
	res, err := moduleAixFilesystem(context.Background(), conn, map[string]any{
		"filesystem": "/newfs", "state": "absent", "rm_mount_point": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixFilesystemRemoveNotExist(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"mount | grep -qFw -- /newfs": {RC: 1},
		"lsfs -l /newfs":              {RC: 1, Stderr: "No record matching /newfs"},
	})
	res, err := moduleAixFilesystem(context.Background(), conn, map[string]any{
		"filesystem": "/newfs", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixFilesystemMissingDeviceAndVG(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"mount | grep -qFw -- /newfs": {RC: 1},
		"lsfs -l /newfs":              {RC: 1, Stderr: "No record matching /newfs"},
	})
	res, err := moduleAixFilesystem(context.Background(), conn, map[string]any{
		"filesystem": "/newfs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: device and/or vg required")
	}
}

func TestModuleAixFilesystemMissingFilesystem(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAixFilesystem(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing filesystem")
	}
}
