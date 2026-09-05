package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMemsetMemstoreInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                               {RC: 0},
		"ma-shell -k AAAAAA memstore.usage name mstestyaa1": {RC: 0, Stdout: `{"bytes":3860997965,"objs":1000}`},
	})
	res, err := moduleMemsetMemstoreInfo(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "mstestyaa1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	meta, _ := res.Extra["memset_api"].(map[string]any)
	if meta["objs"] != float64(1000) {
		t.Fatalf("meta = %+v", res.Extra["memset_api"])
	}
}

func TestModuleMemsetMemstoreInfoMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell": {RC: 1},
	})
	res, err := moduleMemsetMemstoreInfo(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "mstestyaa1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleMemsetMemstoreInfoAPIFault(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                               {RC: 0},
		"ma-shell -k AAAAAA memstore.usage name mstestyaa1": {RC: 0, Stdout: "memstore.usage: <Fault 1: 'not found'>"},
	})
	res, err := moduleMemsetMemstoreInfo(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "mstestyaa1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleMemsetMemstoreInfoMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMemsetMemstoreInfo(context.Background(), conn, map[string]any{
		"name": "mstestyaa1",
	}); err == nil {
		t.Fatal("want error for missing api_key")
	}
	if _, err := moduleMemsetMemstoreInfo(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA",
	}); err == nil {
		t.Fatal("want error for missing name")
	}
}
