package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLvgRename(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg_orig_name 2>/dev/null": {RC: 0, Stdout: "  /dev/sdb1\n"},
		"vgs --noheadings -o pv_name vg_new_name 2>/dev/null":  {RC: 5},
		"vgrename vg_orig_name vg_new_name":                    {RC: 0},
	})
	res, err := moduleLvgRename(context.Background(), conn, map[string]any{
		"vg": "vg_orig_name", "vg_new": "vg_new_name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgRenameAlreadyDone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg_orig_name 2>/dev/null": {RC: 5},
		"vgs --noheadings -o pv_name vg_new_name 2>/dev/null":  {RC: 0, Stdout: "  /dev/sdb1\n"},
	})
	res, err := moduleLvgRename(context.Background(), conn, map[string]any{
		"vg": "vg_orig_name", "vg_new": "vg_new_name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvgRenameNeitherExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg_orig_name 2>/dev/null": {RC: 5},
		"vgs --noheadings -o pv_name vg_new_name 2>/dev/null":  {RC: 5},
	})
	res, err := moduleLvgRename(context.Background(), conn, map[string]any{
		"vg": "vg_orig_name", "vg_new": "vg_new_name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed when neither VG exists, res = %+v", res)
	}
}

func TestModuleLvgRenameBothExist(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vgs --noheadings -o pv_name vg_orig_name 2>/dev/null": {RC: 0, Stdout: "  /dev/sdb1\n"},
		"vgs --noheadings -o pv_name vg_new_name 2>/dev/null":  {RC: 0, Stdout: "  /dev/sdc1\n"},
	})
	res, err := moduleLvgRename(context.Background(), conn, map[string]any{
		"vg": "vg_orig_name", "vg_new": "vg_new_name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed when both VGs exist, res = %+v", res)
	}
}

func TestModuleLvgRenameMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLvgRename(context.Background(), conn, map[string]any{"vg": "x"}); err == nil {
		t.Fatal("want error for missing vg_new")
	}
	if _, err := moduleLvgRename(context.Background(), conn, map[string]any{"vg_new": "y"}); err == nil {
		t.Fatal("want error for missing vg")
	}
}
