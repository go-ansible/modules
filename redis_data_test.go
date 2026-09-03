package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRedisDataSetNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls GET foo":     {RC: 0, Stdout: "\n"},
		"redis-cli -h localhost -p 6379 --tls SET foo bar": {RC: 0, Stdout: "OK\n"},
	})
	res, err := moduleRedisData(context.Background(), conn, map[string]any{"key": "foo", "value": "bar"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRedisDataSetUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls GET foo": {RC: 0, Stdout: "bar\n"},
	})
	res, err := moduleRedisData(context.Background(), conn, map[string]any{"key": "foo", "value": "bar"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRedisDataSetWithExpirationAlwaysChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls GET foo":              {RC: 0, Stdout: "bar\n"},
		"redis-cli -h localhost -p 6379 --tls SET foo bar PX 30000": {RC: 0, Stdout: "OK\n"},
	})
	res, err := moduleRedisData(context.Background(), conn, map[string]any{
		"key": "foo", "value": "bar", "expiration": 30000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed because expiration forces a SET")
	}
}

func TestModuleRedisDataSetNxFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls GET foo":        {RC: 0, Stdout: "existing\n"},
		"redis-cli -h localhost -p 6379 --tls SET foo bar NX": {RC: 0, Stdout: "\n"},
	})
	res, err := moduleRedisData(context.Background(), conn, map[string]any{
		"key": "foo", "value": "bar", "non_existing": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when NX condition isn't met")
	}
}

func TestModuleRedisDataAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls DEL foo": {RC: 0, Stdout: "1\n"},
	})
	res, err := moduleRedisData(context.Background(), conn, map[string]any{"key": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRedisDataAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"redis-cli -h localhost -p 6379 --tls DEL foo": {RC: 0, Stdout: "0\n"},
	})
	res, err := moduleRedisData(context.Background(), conn, map[string]any{"key": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRedisDataMissingValue(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRedisData(context.Background(), conn, map[string]any{"key": "foo"}); err == nil {
		t.Fatal("want error for missing value when state=present")
	}
}
