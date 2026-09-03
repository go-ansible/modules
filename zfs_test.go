package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleZfsCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/myfs 2>/dev/null": {RC: 1},
		"zfs create rpool/myfs":                      {RC: 0},
	})
	res, err := moduleZfs(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZfsCreateWithProperties(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/myfs 2>/dev/null":        {RC: 1},
		"zfs create rpool/myfs":                             {RC: 0},
		"zfs get -H -o value setuid rpool/myfs 2>/dev/null": {RC: 0, Stdout: "on\n"},
		"zfs set setuid=off rpool/myfs":                     {RC: 0},
	})
	res, err := moduleZfs(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "state": "present",
		"extra_zfs_properties": map[string]any{"setuid": "off"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZfsCreateSnapshot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/myfs@mysnapshot 2>/dev/null": {RC: 1},
		"zfs snapshot rpool/myfs@mysnapshot":                    {RC: 0},
	})
	res, err := moduleZfs(context.Background(), conn, map[string]any{
		"name": "rpool/myfs@mysnapshot", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZfsClone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/cloned_fs 2>/dev/null": {RC: 1},
		"zfs clone rpool/myfs@mysnapshot rpool/cloned_fs": {RC: 0},
	})
	res, err := moduleZfs(context.Background(), conn, map[string]any{
		"name": "rpool/cloned_fs", "state": "present", "origin": "rpool/myfs@mysnapshot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZfsPropertyAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/myfs 2>/dev/null":        {RC: 0, Stdout: "rpool/myfs\n"},
		"zfs get -H -o value setuid rpool/myfs 2>/dev/null": {RC: 0, Stdout: "off\n"},
	})
	res, err := moduleZfs(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "state": "present",
		"extra_zfs_properties": map[string]any{"setuid": "off"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleZfsDestroy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/myfs 2>/dev/null": {RC: 0, Stdout: "rpool/myfs\n"},
		"zfs destroy rpool/myfs":                     {RC: 0},
	})
	res, err := moduleZfs(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZfsDestroyAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/myfs 2>/dev/null": {RC: 1},
	})
	res, err := moduleZfs(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleZfsMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZfs(context.Background(), conn, map[string]any{"name": "rpool/myfs"}); err == nil {
		t.Fatal("want error for missing state")
	}
	if _, err := moduleZfs(context.Background(), conn, map[string]any{"state": "present"}); err == nil {
		t.Fatal("want error for missing name")
	}
}
