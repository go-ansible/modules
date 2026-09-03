package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLvolCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 5},
		"lvcreate --size 512 -n test firefly":                                               {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "size": "512",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolCreateMissingSize(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 5},
	})
	if _, err := moduleLvol(context.Background(), conn, map[string]any{"vg": "firefly", "lv": "test"}); err == nil {
		t.Fatal("want error for missing size on create")
	}
}

func TestModuleLvolAlreadyUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "536870912 -wi-ao----\n"},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "size": "512",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLvolGrow(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "536870912 -wi-ao----\n"},
		"lvresize --size 1024 firefly/test":                                                 {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "size": "1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolShrinkWithoutForceFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "1073741824 -wi-ao----\n"},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "size": "512",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed for shrink without force, res = %+v", res)
	}
}

func TestModuleLvolShrinkWithForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "1073741824 -wi-ao----\n"},
		"lvresize --size 512 -f firefly/test":                                               {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "size": "512", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolShrinkDisabledSkips(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "1073741824 -wi-ao----\n"},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "size": "512", "shrink": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want no-op when shrink=false, res = %+v", res)
	}
}

func TestModuleLvolPercentAlwaysChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "536870912 -wi-ao----\n"},
		"lvresize --extents 100%FREE firefly/test":                                          {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "size": "100%FREE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolRemoveWithoutForceFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "536870912 -wi-ao----\n"},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed for remove without force, res = %+v", res)
	}
}

func TestModuleLvolRemoveWithForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "536870912 -wi-ao----\n"},
		"lvremove -f firefly/test": {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "state": "absent", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolRemoveAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 5},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLvolDeactivate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 0, Stdout: "536870912 -wi-ao----\n"},
		"lvchange -an firefly/test": {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "active": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolThinPoolCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/testpool 2>/dev/null": {RC: 5},
		"lvcreate -T --size 512g firefly/testpool":                                              {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "thinpool": "testpool", "size": "512g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolThinVolumeCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/test 2>/dev/null": {RC: 5},
		"lvcreate -T firefly/testpool -V 128g -n test":                                      {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "thinpool": "testpool", "size": "128g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolSnapshotCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lvs --noheadings -o lv_size,lv_attr --units b --nosuffix firefly/snap1 2>/dev/null": {RC: 5},
		"lvcreate -s --size 100m -n snap1 firefly/test":                                      {RC: 0},
	})
	res, err := moduleLvol(context.Background(), conn, map[string]any{
		"vg": "firefly", "lv": "test", "snapshot": "snap1", "size": "100m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvolMissingIdentity(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLvol(context.Background(), conn, map[string]any{"vg": "firefly"}); err == nil {
		t.Fatal("want error when none of lv/thinpool/snapshot given")
	}
}
