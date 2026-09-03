package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRedisDataIncrDefault(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls INCR foo": {RC: 0, Stdout: "4\n"},
	})
	res, err := moduleRedisDataIncr(context.Background(), conn, map[string]any{"key": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["value"] != float64(4) {
		t.Fatalf("value = %v", res.Extra["value"])
	}
}

func TestModuleRedisDataIncrInt(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls INCRBY foo 5": {RC: 0, Stdout: "15\n"},
	})
	res, err := moduleRedisDataIncr(context.Background(), conn, map[string]any{"key": "foo", "increment_int": 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != float64(15) {
		t.Fatalf("value = %v", res.Extra["value"])
	}
}

func TestModuleRedisDataIncrFloat(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls INCRBYFLOAT foo 20.4": {RC: 0, Stdout: "65.9\n"},
	})
	res, err := moduleRedisDataIncr(context.Background(), conn, map[string]any{"key": "foo", "increment_float": 20.4})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != float64(65.9) {
		t.Fatalf("value = %v", res.Extra["value"])
	}
}

func TestModuleRedisDataIncrMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleRedisDataIncr(context.Background(), conn, map[string]any{
		"key": "foo", "increment_int": 1, "increment_float": 1.0,
	})
	if err == nil {
		t.Fatal("want error for increment_int and increment_float together")
	}
}

func TestModuleRedisDataIncrNotIncrementable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls INCR foo": {RC: 1, Stderr: "ERR value is not an integer or out of range"},
	})
	res, err := moduleRedisDataIncr(context.Background(), conn, map[string]any{"key": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}
