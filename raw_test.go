package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRawRunsDirectly(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"echo hi | cat": {RC: 0, Stdout: "hi\n"},
	})
	res, err := moduleRaw(context.Background(), conn, map[string]any{"cmd": "echo hi | cat"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["stdout"] != "hi\n" {
		t.Fatalf("stdout = %v", res.Extra["stdout"])
	}
}

func TestModuleRawUsesRawParams(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uptime": {RC: 0},
	})
	res, err := moduleRaw(context.Background(), conn, map[string]any{"_raw_params": "uptime"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRawNonZeroExit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"false": {RC: 1},
	})
	res, err := moduleRaw(context.Background(), conn, map[string]any{"cmd": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}

func TestModuleRawMissingCmd(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRaw(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
