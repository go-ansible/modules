package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLxdStoragePoolInfoAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET '/1.0/storage-pools?recursion=1'": {RC: 0, Stdout: `[
			{"name":"default","driver":"dir","description":"Default storage pool","config":{"source":"/var/lib/lxd/storage-pools/default"},"locations":["none"],"status":"Created","used_by":["/1.0/instances/container1"]},
			{"name":"zpool","driver":"zfs","description":"","config":{},"locations":["none"],"status":"Created","used_by":[]}
		]`},
	})
	res, err := moduleLxdStoragePoolInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	pools := res.Extra["storage_pools"].([]any)
	if len(pools) != 2 {
		t.Fatalf("got %d pools, want 2", len(pools))
	}
}

func TestModuleLxdStoragePoolInfoByName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET '/1.0/storage-pools?recursion=1'": {RC: 0, Stdout: `[
			{"name":"default","driver":"dir","description":"","config":{},"locations":["none"],"status":"Created","used_by":[]},
			{"name":"zpool","driver":"zfs","description":"","config":{},"locations":["none"],"status":"Created","used_by":[]}
		]`},
	})
	res, err := moduleLxdStoragePoolInfo(context.Background(), conn, map[string]any{"name": "zpool"})
	if err != nil {
		t.Fatal(err)
	}
	pools := res.Extra["storage_pools"].([]any)
	if len(pools) != 1 {
		t.Fatalf("got %d pools, want 1", len(pools))
	}
	entry := pools[0].(map[string]any)
	if entry["name"] != "zpool" {
		t.Fatalf("name = %v", entry["name"])
	}
}

func TestModuleLxdStoragePoolInfoByType(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET '/1.0/storage-pools?recursion=1'": {RC: 0, Stdout: `[
			{"name":"default","driver":"dir","description":"","config":{},"locations":["none"],"status":"Created","used_by":[]},
			{"name":"zpool","driver":"zfs","description":"","config":{},"locations":["none"],"status":"Created","used_by":[]}
		]`},
	})
	res, err := moduleLxdStoragePoolInfo(context.Background(), conn, map[string]any{"type": []any{"zfs", "btrfs"}})
	if err != nil {
		t.Fatal(err)
	}
	pools := res.Extra["storage_pools"].([]any)
	if len(pools) != 1 {
		t.Fatalf("got %d pools, want 1", len(pools))
	}
}

func TestModuleLxdStoragePoolInfoQueryFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET '/1.0/storage-pools?recursion=1'": {RC: 1, Stderr: "not found"},
	})
	res, err := moduleLxdStoragePoolInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}
