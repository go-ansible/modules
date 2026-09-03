package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulKvInfoFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul kv get -http-addr=http://localhost:8500 -detailed somekey": {
			RC:     0,
			Stdout: "CreateIndex      336\nFlags            0\nKey              somekey\nLockIndex        0\nModifyIndex      336\nSession          -\nValue            somevalue\n",
		},
	})
	res, err := moduleConsulKvInfo(context.Background(), conn, map[string]any{"key": "somekey"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	data, ok := res.Extra["data"].([]map[string]any)
	if !ok || len(data) != 1 {
		t.Fatalf("data = %#v", res.Extra["data"])
	}
	if data[0]["Value"] != "somevalue" || data[0]["Key"] != "somekey" {
		t.Fatalf("data[0] = %#v", data[0])
	}
}

func TestModuleConsulKvInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul kv get -http-addr=http://localhost:8500 -detailed somekey": {RC: 1, Stderr: "Error! No key exists at: somekey"},
	})
	res, err := moduleConsulKvInfo(context.Background(), conn, map[string]any{"key": "somekey"})
	if err != nil {
		t.Fatal(err)
	}
	data, ok := res.Extra["data"].([]map[string]any)
	if !ok || len(data) != 0 {
		t.Fatalf("data = %#v", res.Extra["data"])
	}
}

func TestModuleConsulKvInfoRecurse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul kv get -http-addr=http://localhost:8500 -detailed -recurse app/": {
			RC:     0,
			Stdout: "Key              app/a\nValue            1\n\nKey              app/b\nValue            2\n",
		},
	})
	res, err := moduleConsulKvInfo(context.Background(), conn, map[string]any{"key": "app/", "recurse": true})
	if err != nil {
		t.Fatal(err)
	}
	data, ok := res.Extra["data"].([]map[string]any)
	if !ok || len(data) != 2 {
		t.Fatalf("data = %#v", res.Extra["data"])
	}
}

func TestModuleConsulKvInfoMissingKey(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulKvInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing key")
	}
}
