package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePmemCreateNamespaces(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ndctl list -N": {RC: 0, Stdout: ""},
		"ndctl create-namespace -m raw -t pmem -s 1GB":      {RC: 0},
		"ndctl create-namespace -m sector -t pmem -s 320MB": {RC: 0},
	})
	res, err := modulePmem(context.Background(), conn, map[string]any{
		"namespace": []any{
			map[string]any{"size": "1GB", "type": "pmem", "mode": "raw"},
			map[string]any{"size": "320MB", "type": "pmem", "mode": "sector"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePmemRemovesExistingFirst(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ndctl list -N":                        {RC: 0, Stdout: `[{"dev":"namespace0.0"}]`},
		"ndctl disable-namespace namespace0.0": {RC: 0},
		"ndctl destroy-namespace namespace0.0": {RC: 0},
		"ndctl create-namespace -m raw":        {RC: 0},
	})
	res, err := modulePmem(context.Background(), conn, map[string]any{
		"namespace": []any{map[string]any{"mode": "raw"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 4 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePmemAppendSkipsRemoval(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ndctl create-namespace -m raw": {RC: 0},
	})
	res, err := modulePmem(context.Background(), conn, map[string]any{
		"namespace":        []any{map[string]any{"mode": "raw"}},
		"namespace_append": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no ndctl list -N when appending", conn.Commands)
	}
}

func TestModulePmemRegionArgsNotImplemented(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := modulePmem(context.Background(), conn, map[string]any{
		"appdirect": 10, "memorymode": 70,
	})
	if err == nil {
		t.Fatal("want error: region-goal provisioning is not implemented")
	}
}

func TestModulePmemBadMode(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := modulePmem(context.Background(), conn, map[string]any{
		"namespace": []any{map[string]any{"mode": "weird"}},
	})
	if err == nil {
		t.Fatal("want error for invalid namespace mode")
	}
}

func TestModulePmemNothingGiven(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := modulePmem(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error when neither namespace nor region args are given")
	}
}
