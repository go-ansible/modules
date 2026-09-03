package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLvmPvCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_name /dev/sdb 2>/dev/null": {RC: 5},
		"test -e /dev/sdb":  {RC: 0},
		"pvcreate /dev/sdb": {RC: 0},
	})
	res, err := moduleLvmPv(context.Background(), conn, map[string]any{"device": "/dev/sdb"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvmPvCreateMissingDevice(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_name /dev/sdb 2>/dev/null": {RC: 5},
		"test -e /dev/sdb": {RC: 1},
	})
	res, err := moduleLvmPv(context.Background(), conn, map[string]any{"device": "/dev/sdb"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed for missing device, res = %+v", res)
	}
}

func TestModuleLvmPvAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_name /dev/sdb 2>/dev/null": {RC: 0, Stdout: "/dev/sdb\n"},
	})
	res, err := moduleLvmPv(context.Background(), conn, map[string]any{"device": "/dev/sdb"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLvmPvResize(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_name /dev/sdb 2>/dev/null": {RC: 0, Stdout: "/dev/sdb\n"},
		"pvresize /dev/sdb": {RC: 0},
	})
	res, err := moduleLvmPv(context.Background(), conn, map[string]any{"device": "/dev/sdb", "resize": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvmPvAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_name /dev/sdb 2>/dev/null": {RC: 0, Stdout: "/dev/sdb\n"},
		"pvremove /dev/sdb": {RC: 0},
	})
	res, err := moduleLvmPv(context.Background(), conn, map[string]any{"device": "/dev/sdb", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvmPvAbsentForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_name /dev/sdb 2>/dev/null": {RC: 0, Stdout: "/dev/sdb\n"},
		"pvremove -ff /dev/sdb":                            {RC: 0},
	})
	res, err := moduleLvmPv(context.Background(), conn, map[string]any{"device": "/dev/sdb", "state": "absent", "force": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvmPvAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_name /dev/sdb 2>/dev/null": {RC: 5},
	})
	res, err := moduleLvmPv(context.Background(), conn, map[string]any{"device": "/dev/sdb", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLvmPvMissingDevice(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLvmPv(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing device")
	}
}
