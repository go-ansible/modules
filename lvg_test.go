package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLvgCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 5},
		"pvs --noheadings -o pv_name /dev/sdb 2>/dev/null":    {RC: 5},
		"pvcreate /dev/sdb":                  {RC: 0},
		"vgcreate -s 4 vg.services /dev/sdb": {RC: 0},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "pvs": []any{"/dev/sdb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgCreateMissingPvs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 5},
	})
	if _, err := moduleLvg(context.Background(), conn, map[string]any{"vg": "vg.services"}); err == nil {
		t.Fatal("want error when creating a VG with no pvs")
	}
}

func TestModuleLvgExtend(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 0, Stdout: "  /dev/sdb1\n"},
		"pvs --noheadings -o pv_name /dev/sdc5 2>/dev/null":   {RC: 5},
		"pvcreate /dev/sdc5":             {RC: 0},
		"vgextend vg.services /dev/sdc5": {RC: 0},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "pvs": []any{"/dev/sdb1", "/dev/sdc5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgReduceExtraPVs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 0, Stdout: "  /dev/sda5\n  /dev/sdb1\n"},
		"vgreduce vg.services /dev/sda5":                      {RC: 0},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "pvs": []any{"/dev/sdb1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgKeepExtraPVsWhenDisabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 0, Stdout: "  /dev/sda5\n  /dev/sdb1\n"},
		"pvs --noheadings -o pv_name /dev/sdc1 2>/dev/null":   {RC: 5},
		"pvcreate /dev/sdc1":             {RC: 0},
		"vgextend vg.services /dev/sdc1": {RC: 0},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "pvs": []any{"/dev/sdb1", "/dev/sdc1"}, "remove_extra_pvs": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	for _, c := range conn.Commands {
		if c == "vgreduce vg.services /dev/sda5" {
			t.Fatalf("want /dev/sda5 kept when remove_extra_pvs=false, commands = %v", conn.Commands)
		}
	}
}

func TestModuleLvgAlreadyUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 0, Stdout: "  /dev/sdb1\n"},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "pvs": []any{"/dev/sdb1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLvgAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 0, Stdout: "  /dev/sdb1\n"},
		"vgremove vg.services":                                {RC: 0},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgAbsentForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 0, Stdout: "  /dev/sdb1\n"},
		"vgremove -f vg.services":                             {RC: 0},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "state": "absent", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 5},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLvgActive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 0, Stdout: "  /dev/sdb1\n"},
		"vgchange -a y vg.services":                           {RC: 0},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "state": "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgResetUUIDNotIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg.services 2>/dev/null": {RC: 0, Stdout: "  /dev/sdb1\n"},
		"vgchange -u vg.services":                             {RC: 0},
	})
	res, err := moduleLvg(context.Background(), conn, map[string]any{
		"vg": "vg.services", "reset_vg_uuid": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgMissingVG(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLvg(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing vg")
	}
}
