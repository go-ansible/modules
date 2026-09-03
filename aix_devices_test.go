package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAixDevicesDiscoverAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cfgmgr": {RC: 0, Stdout: "ok"},
	})
	res, err := moduleAixDevices(context.Background(), conn, map[string]any{"device": "all", "state": "available"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "cfgmgr" {
		t.Fatalf("commands = %v, want plain cfgmgr for device=all", conn.Commands)
	}
}

func TestModuleAixDevicesDiscoverSpecific(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsdev -C -l vio0": {RC: 0, Stdout: "vio0 Available\n"},
		"cfgmgr -l vio0":   {RC: 0, Stdout: "ok"},
	})
	res, err := moduleAixDevices(context.Background(), conn, map[string]any{"device": "vio0", "state": "available"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixDevicesRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsdev -C -l ent2": {RC: 0, Stdout: "ent2 Available\n"},
		"rmdev -l ent2 -d": {RC: 0, Stdout: "ent2 deleted\n"},
	})
	res, err := moduleAixDevices(context.Background(), conn, map[string]any{"device": "ent2", "state": "removed"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixDevicesDefinedAlreadyDefined(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsdev -C -l en2": {RC: 0, Stdout: "en2 Defined\n"},
	})
	res, err := moduleAixDevices(context.Background(), conn, map[string]any{"device": "en2", "state": "defined"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixDevicesRemoveNotExist(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsdev -C -l ent4": {RC: 0, Stdout: ""},
	})
	res, err := moduleAixDevices(context.Background(), conn, map[string]any{"device": "ent4", "state": "removed"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixDevicesRemoveMissingDevice(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleAixDevices(context.Background(), conn, map[string]any{"state": "removed"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: device required for removed state")
	}
}

func TestModuleAixDevicesChangeAttrs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsdev -C -l en1":          {RC: 0, Stdout: "en1 Available\n"},
		"lsattr -El en1 -a mtu":    {RC: 0, Stdout: "mtu 1500 Maximum True\n"},
		"chdev -l en1 -a mtu=9000": {RC: 0, Stdout: "en1 changed\n"},
	})
	res, err := moduleAixDevices(context.Background(), conn, map[string]any{
		"device": "en1", "attributes": map[string]any{"mtu": "9000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}
