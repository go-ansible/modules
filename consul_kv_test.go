package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulKvSetNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul kv get -http-addr=http://localhost:8500 somekey":           {RC: 1},
		"consul kv put -http-addr=http://localhost:8500 somekey somevalue": {RC: 0, Stdout: "Success! Data written to: somekey\n"},
		"consul kv get -http-addr=http://localhost:8500 -detailed somekey": {RC: 0, Stdout: "Value            somevalue\nModifyIndex      42\n"},
	})
	res, err := moduleConsulKv(context.Background(), conn, map[string]any{"key": "somekey", "value": "somevalue"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, ok := res.Extra["data"].([]map[string]any)
	if !ok || len(data) != 1 || data[0]["Value"] != "somevalue" {
		t.Fatalf("data = %#v", res.Extra["data"])
	}
}

func TestModuleConsulKvSetUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul kv get -http-addr=http://localhost:8500 somekey":           {RC: 0, Stdout: "somevalue\n"},
		"consul kv get -http-addr=http://localhost:8500 -detailed somekey": {RC: 0, Stdout: "Value            somevalue\n"},
	})
	res, err := moduleConsulKv(context.Background(), conn, map[string]any{"key": "somekey", "value": "somevalue"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleConsulKvAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul kv get -http-addr=http://localhost:8500 -detailed somekey": {RC: 0, Stdout: "Value            somevalue\n"},
		"consul kv delete -http-addr=http://localhost:8500 somekey":        {RC: 0, Stdout: "Success! Deleted key: somekey\n"},
	})
	res, err := moduleConsulKv(context.Background(), conn, map[string]any{"key": "somekey", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleConsulKvAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul kv get -http-addr=http://localhost:8500 -detailed somekey": {RC: 1},
	})
	res, err := moduleConsulKv(context.Background(), conn, map[string]any{"key": "somekey", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleConsulKvAcquireRequiresSession(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleConsulKv(context.Background(), conn, map[string]any{"key": "somekey", "state": "acquire"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed without a session")
	}
}

func TestModuleConsulKvTokenViaEnv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"CONSUL_HTTP_TOKEN=sekret consul kv get -http-addr=http://localhost:8500 somekey":           {RC: 1},
		"CONSUL_HTTP_TOKEN=sekret consul kv put -http-addr=http://localhost:8500 somekey v":         {RC: 0},
		"CONSUL_HTTP_TOKEN=sekret consul kv get -http-addr=http://localhost:8500 -detailed somekey": {RC: 0, Stdout: "Value            v\n"},
	})
	res, err := moduleConsulKv(context.Background(), conn, map[string]any{"key": "somekey", "value": "v", "token": "sekret"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	for _, cmd := range conn.Commands {
		if cmd == "" {
			t.Fatal("empty command recorded")
		}
	}
}

func TestModuleConsulKvMissingKey(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulKv(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing key")
	}
}
