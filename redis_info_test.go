package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRedisInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 INFO": {RC: 0, Stdout: "# Server\r\nredis_version:7.2.0\r\nconnected_clients:3\r\n\r\n"},
	})
	res, err := moduleRedisInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	info, ok := res.Extra["info"].(map[string]any)
	if !ok {
		t.Fatalf("info = %#v", res.Extra["info"])
	}
	if info["redis_version"] != "7.2.0" {
		t.Fatalf("redis_version = %v", info["redis_version"])
	}
}

func TestModuleRedisInfoCluster(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 INFO":         {RC: 0, Stdout: "redis_version:7.2.0\r\n"},
		"redis-cli -h localhost -p 6379 CLUSTER INFO": {RC: 0, Stdout: "cluster_enabled:1\r\ncluster_state:ok\r\n"},
	})
	res, err := moduleRedisInfo(context.Background(), conn, map[string]any{"cluster": true})
	if err != nil {
		t.Fatal(err)
	}
	cl, ok := res.Extra["cluster_info"].(map[string]any)
	if !ok {
		t.Fatalf("cluster_info = %#v", res.Extra["cluster_info"])
	}
	if cl["cluster_state"] != "ok" {
		t.Fatalf("cluster_state = %v", cl["cluster_state"])
	}
}

func TestModuleRedisInfoConnectFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 INFO": {RC: 1, Stderr: "Could not connect to Redis"},
	})
	res, err := moduleRedisInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}
