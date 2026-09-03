package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAixLvgCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg -o":                         {RC: 0, Stdout: "rootvg\n"},
		"lsvg":                            {RC: 0, Stdout: "rootvg\n"},
		"lspv":                            {RC: 0, Stdout: "hdisk1 000123 None\n"},
		"mkvg -S -s 128 -y datavg hdisk1": {RC: 0},
	})
	res, err := moduleAixLvg(context.Background(), conn, map[string]any{
		"vg": "datavg", "pvs": []any{"hdisk1"}, "pp_size": 128, "vg_type": "scalable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixLvgExtend(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg -o":                {RC: 0, Stdout: "rootvg\n"},
		"lsvg":                   {RC: 0, Stdout: "rootvg\n"},
		"lspv":                   {RC: 0, Stdout: "hdisk1 000123 None\n"},
		"extendvg rootvg hdisk1": {RC: 0},
	})
	res, err := moduleAixLvg(context.Background(), conn, map[string]any{
		"vg": "rootvg", "pvs": []any{"hdisk1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixLvgPVUsedByAnotherVG(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg -o": {RC: 0, Stdout: ""},
		"lsvg":    {RC: 0, Stdout: ""},
		"lspv":    {RC: 0, Stdout: "hdisk1 000123 othervg\n"},
	})
	res, err := moduleAixLvg(context.Background(), conn, map[string]any{
		"vg": "datavg", "pvs": []any{"hdisk1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleAixLvgRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg -o":                    {RC: 0, Stdout: "datavg\n"},
		"lsvg":                       {RC: 0, Stdout: "datavg\n"},
		"lsvg -p datavg":             {RC: 0, Stdout: "datavg:\nPV_NAME PV STATE TOTAL PPs\nhdisk1 active 100\n"},
		"reducevg -df datavg hdisk1": {RC: 0},
	})
	res, err := moduleAixLvg(context.Background(), conn, map[string]any{
		"vg": "datavg", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixLvgAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg -o": {RC: 0, Stdout: ""},
		"lsvg":    {RC: 0, Stdout: ""},
	})
	res, err := moduleAixLvg(context.Background(), conn, map[string]any{
		"vg": "datavg", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixLvgVaryon(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg -o":         {RC: 0, Stdout: ""},
		"lsvg":            {RC: 0, Stdout: "datavg\n"},
		"varyonvg datavg": {RC: 0},
	})
	res, err := moduleAixLvg(context.Background(), conn, map[string]any{
		"vg": "datavg", "state": "varyon",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixLvgMissingVG(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAixLvg(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing vg")
	}
}

func TestModuleAixLvgCreateMissingPvs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg -o": {RC: 0, Stdout: ""},
		"lsvg":    {RC: 0, Stdout: ""},
	})
	res, err := moduleAixLvg(context.Background(), conn, map[string]any{"vg": "datavg"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: pvs required for state=present")
	}
}
