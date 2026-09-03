package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleVdoCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1":                    {RC: 1},
		"vdo create --name=vdo1 --device=/dev/md0 --logicalsize=2T": {RC: 0},
	})
	res, err := moduleVdo(context.Background(), conn, map[string]any{
		"name": "vdo1", "device": "/dev/md0", "logicalsize": "2T",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVdoCreateMissingDevice(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1": {RC: 1},
	})
	if _, err := moduleVdo(context.Background(), conn, map[string]any{"name": "vdo1"}); err == nil {
		t.Fatal("want error for missing device on create")
	}
}

func TestModuleVdoRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1": {RC: 0},
		"vdo remove --name=vdo1":                 {RC: 0},
	})
	res, err := moduleVdo(context.Background(), conn, map[string]any{"name": "vdo1", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVdoRemoveAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleVdo(context.Background(), conn, map[string]any{"name": "vdo1", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleVdoModifyExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1":  {RC: 0},
		"vdo modify --name=vdo1 --logicalsize=4T": {RC: 0},
	})
	res, err := moduleVdo(context.Background(), conn, map[string]any{
		"name": "vdo1", "logicalsize": "4T",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVdoCompressionToggle(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1": {RC: 0},
		"vdo disableCompression --name=vdo1":     {RC: 0},
	})
	res, err := moduleVdo(context.Background(), conn, map[string]any{
		"name": "vdo1", "compression": "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVdoStartStop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1": {RC: 0},
		"vdo stop --name=vdo1":                   {RC: 0},
	})
	res, err := moduleVdo(context.Background(), conn, map[string]any{
		"name": "vdo1", "running": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVdoActivateDeactivate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1": {RC: 0},
		"vdo deactivate --name=vdo1":             {RC: 0},
	})
	res, err := moduleVdo(context.Background(), conn, map[string]any{
		"name": "vdo1", "activated": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVdoNoOpWhenExistingAndNoArgs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vdo status --name=vdo1 >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleVdo(context.Background(), conn, map[string]any{"name": "vdo1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleVdoMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleVdo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
