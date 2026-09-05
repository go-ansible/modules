package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMemsetServerInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                          {RC: 0},
		"ma-shell -k AAAAAA server.info name testyaa1": {RC: 0, Stdout: `{"name":"testyaa1","status":"LIVE"}`},
	})
	res, err := moduleMemsetServerInfo(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "testyaa1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	meta, _ := res.Extra["memset_api"].(map[string]any)
	if meta["status"] != "LIVE" {
		t.Fatalf("meta = %+v", res.Extra["memset_api"])
	}
}

func TestModuleMemsetServerInfoMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell": {RC: 1},
	})
	res, err := moduleMemsetServerInfo(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "testyaa1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleMemsetServerInfoAuthFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                          {RC: 0},
		"ma-shell -k BADKEY server.info name testyaa1": {RC: 2, Stderr: "Failed to connect to the server"},
	})
	res, err := moduleMemsetServerInfo(context.Background(), conn, map[string]any{
		"api_key": "BADKEY", "name": "testyaa1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
