package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRedisFlushAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 FLUSHALL": {RC: 0, Stdout: "OK\n"},
	})
	res, err := moduleRedis(context.Background(), conn, map[string]any{"command": "flush"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRedisFlushDb(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 -n 2 FLUSHDB": {RC: 0, Stdout: "OK\n"},
	})
	res, err := moduleRedis(context.Background(), conn, map[string]any{
		"command": "flush", "flush_mode": "db", "db": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["db"] != 2 {
		t.Fatalf("db = %v", res.Extra["db"])
	}
}

func TestModuleRedisFlushDbMissingDb(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRedis(context.Background(), conn, map[string]any{"command": "flush", "flush_mode": "db"}); err == nil {
		t.Fatal("want error for missing db")
	}
}

func TestModuleRedisConfigChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 CONFIG GET maxmemory":       {RC: 0, Stdout: "maxmemory\n0\n"},
		"redis-cli -h localhost -p 6379 CONFIG SET maxmemory 100mb": {RC: 0, Stdout: "OK\n"},
	})
	res, err := moduleRedis(context.Background(), conn, map[string]any{
		"command": "config", "name": "maxmemory", "value": "100mb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRedisConfigUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 CONFIG GET maxmemory": {RC: 0, Stdout: "maxmemory\n100mb\n"},
	})
	res, err := moduleRedis(context.Background(), conn, map[string]any{
		"command": "config", "name": "maxmemory", "value": "100mb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	for _, cmd := range conn.Commands {
		if cmd == "redis-cli -h localhost -p 6379 CONFIG SET maxmemory 100mb" {
			t.Fatal("CONFIG SET should not have been run")
		}
	}
}

func TestModuleRedisReplicaAlreadyMaster(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 INFO replication": {RC: 0, Stdout: "role:master\n"},
	})
	res, err := moduleRedis(context.Background(), conn, map[string]any{
		"command": "replica", "replica_mode": "master",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRedisReplicaSetsUp(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 INFO replication":      {RC: 0, Stdout: "role:master\n"},
		"redis-cli -h localhost -p 6379 REPLICAOF myhost 6380": {RC: 0, Stdout: "OK\n"},
	})
	res, err := moduleRedis(context.Background(), conn, map[string]any{
		"command": "replica", "master_host": "myhost", "master_port": 6380,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRedisReplicaMissingMasterHost(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRedis(context.Background(), conn, map[string]any{"command": "replica"}); err == nil {
		t.Fatal("want error for missing master_host")
	}
}

func TestModuleRedisPasswordViaEnv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"REDISCLI_AUTH=secret redis-cli -h localhost -p 6379 FLUSHALL": {RC: 0},
	})
	res, err := moduleRedis(context.Background(), conn, map[string]any{
		"command": "flush", "login_password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	for _, cmd := range conn.Commands {
		if cmd == "" || cmd == "redis-cli -h localhost -p 6379 FLUSHALL" {
			t.Fatalf("password leaked or missing from command: %q", cmd)
		}
	}
}

func TestModuleRedisMissingCommand(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRedis(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing command")
	}
}

func TestModuleRedisUnknownCommand(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRedis(context.Background(), conn, map[string]any{"command": "bogus"}); err == nil {
		t.Fatal("want error for unknown command")
	}
}
