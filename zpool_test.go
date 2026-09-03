package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleZpoolCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null": {RC: 1},
		"zpool create tank /dev/sda":             {RC: 0},
	})
	res, err := moduleZpool(context.Background(), conn, map[string]any{
		"name": "tank",
		"vdevs": []any{
			map[string]any{"disks": []any{"/dev/sda"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZpoolCreateMirror(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null":     {RC: 1},
		"zpool create tank mirror /dev/sda /dev/sdb": {RC: 0},
	})
	res, err := moduleZpool(context.Background(), conn, map[string]any{
		"name": "tank",
		"vdevs": []any{
			map[string]any{"type": "mirror", "disks": []any{"/dev/sda", "/dev/sdb"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZpoolCreateWithCacheAndProperties(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null":                         {RC: 1},
		"zpool create -o autoexpand=on tank /dev/sda cache /dev/nvme0n1": {RC: 0},
	})
	res, err := moduleZpool(context.Background(), conn, map[string]any{
		"name": "tank",
		"vdevs": []any{
			map[string]any{"disks": []any{"/dev/sda"}},
			map[string]any{"role": "cache", "disks": []any{"/dev/nvme0n1"}},
		},
		"pool_properties": map[string]any{"autoexpand": "on"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZpoolCreateMissingVdevs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null": {RC: 1},
	})
	if _, err := moduleZpool(context.Background(), conn, map[string]any{"name": "tank"}); err == nil {
		t.Fatal("want error when creating a pool with no vdevs")
	}
}

func TestModuleZpoolReconcileProperties(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null":            {RC: 0, Stdout: "tank\n"},
		"zpool get -H -o value autoexpand tank 2>/dev/null": {RC: 0, Stdout: "off\n"},
		"zpool set autoexpand=on tank":                      {RC: 0},
	})
	res, err := moduleZpool(context.Background(), conn, map[string]any{
		"name": "tank", "pool_properties": map[string]any{"autoexpand": "on"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZpoolAlreadyUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null": {RC: 0, Stdout: "tank\n"},
	})
	res, err := moduleZpool(context.Background(), conn, map[string]any{"name": "tank"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleZpoolDestroy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null": {RC: 0, Stdout: "tank\n"},
		"zpool destroy tank":                     {RC: 0},
	})
	res, err := moduleZpool(context.Background(), conn, map[string]any{"name": "tank", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZpoolDestroyAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null": {RC: 1},
	})
	res, err := moduleZpool(context.Background(), conn, map[string]any{"name": "tank", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleZpoolMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZpool(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
