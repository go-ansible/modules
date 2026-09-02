package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAptKeyAddByKeyserver(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apt-key list 2>/dev/null | grep -qi ABCD1234":                      {RC: 1},
		"apt-key adv --keyserver keyserver.ubuntu.com --recv-keys ABCD1234": {RC: 0},
	})
	res, err := moduleAptKey(context.Background(), conn, map[string]any{
		"id": "ABCD1234", "keyserver": "keyserver.ubuntu.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAptKeyAddByURL(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"curl -fsSL https://example.com/key.asc | apt-key add -": {RC: 0},
	})
	res, err := moduleAptKey(context.Background(), conn, map[string]any{
		"url": "https://example.com/key.asc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptKeyAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apt-key list 2>/dev/null | grep -qi ABCD1234": {RC: 0},
	})
	res, err := moduleAptKey(context.Background(), conn, map[string]any{
		"id": "ABCD1234", "keyserver": "keyserver.ubuntu.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: key already present")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no fetch attempted, commands = %v", conn.Commands)
	}
}

func TestModuleAptKeyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apt-key list 2>/dev/null | grep -qi ABCD1234": {RC: 0},
		"apt-key del ABCD1234":                         {RC: 0},
	})
	res, err := moduleAptKey(context.Background(), conn, map[string]any{
		"id": "ABCD1234", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptKeyAbsentNotPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apt-key list 2>/dev/null | grep -qi ABCD1234": {RC: 1},
	})
	res, err := moduleAptKey(context.Background(), conn, map[string]any{
		"id": "ABCD1234", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAptKeyAbsentMissingID(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAptKey(context.Background(), conn, map[string]any{
		"url": "https://example.com/key.asc", "state": "absent",
	}); err == nil {
		t.Fatal("want error: id required for state=absent")
	}
}

func TestModuleAptKeyMissingIDAndURL(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAptKey(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: at least one of id or url required")
	}
}

func TestModuleAptKeyIDWithoutKeyserverOrURL(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"apt-key list 2>/dev/null | grep -qi ABCD1234": {RC: 1},
	})
	if _, err := moduleAptKey(context.Background(), conn, map[string]any{"id": "ABCD1234"}); err == nil {
		t.Fatal("want error: id alone has no way to fetch the key")
	}
}

func TestModuleAptKeyInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAptKey(context.Background(), conn, map[string]any{
		"id": "ABCD1234", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
