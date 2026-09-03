package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRedisDataInfoExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls GET foo": {RC: 0, Stdout: "bar\n"},
	})
	res, err := moduleRedisDataInfo(context.Background(), conn, map[string]any{"key": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["exists"] != true {
		t.Fatalf("exists = %v", res.Extra["exists"])
	}
	if res.Extra["value"] != "bar" {
		t.Fatalf("value = %v", res.Extra["value"])
	}
}

func TestModuleRedisDataInfoAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls GET foo": {RC: 0, Stdout: "\n"},
	})
	res, err := moduleRedisDataInfo(context.Background(), conn, map[string]any{"key": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["exists"] != false {
		t.Fatalf("exists = %v", res.Extra["exists"])
	}
	if _, ok := res.Extra["value"]; ok {
		t.Fatal("value should not be set when key is absent")
	}
}

func TestModuleRedisDataInfoMissingKey(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRedisDataInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing key")
	}
}
