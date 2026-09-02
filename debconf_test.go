package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDebconfSet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"debconf-show postfix 2>/dev/null | grep -qF 'postfix/mailname: mail.example.com'": {RC: 1},
		"echo 'postfix postfix/mailname string mail.example.com' | debconf-set-selections": {RC: 0},
	})
	res, err := moduleDebconf(context.Background(), conn, map[string]any{
		"name": "postfix", "question": "postfix/mailname", "value": "mail.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDebconfAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"debconf-show postfix 2>/dev/null | grep -qF 'postfix/mailname: mail.example.com'": {RC: 0},
	})
	res, err := moduleDebconf(context.Background(), conn, map[string]any{
		"name": "postfix", "question": "postfix/mailname", "value": "mail.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no set-selections attempted, commands = %v", conn.Commands)
	}
}

func TestModuleDebconfVtype(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"debconf-show foo 2>/dev/null | grep -qF 'foo/enable: true'":  {RC: 1},
		"echo 'foo foo/enable boolean true' | debconf-set-selections": {RC: 0},
	})
	res, err := moduleDebconf(context.Background(), conn, map[string]any{
		"name": "foo", "question": "foo/enable", "value": "true", "vtype": "boolean",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDebconfStateAbsentIsNoop(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleDebconf(context.Background(), conn, map[string]any{
		"name": "foo", "question": "foo/bar", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, want a no-op ok", res)
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no commands run, got %v", conn.Commands)
	}
}

func TestModuleDebconfMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDebconf(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleDebconf(context.Background(), conn, map[string]any{"name": "foo"}); err == nil {
		t.Fatal("want error for missing question")
	}
	if _, err := moduleDebconf(context.Background(), conn, map[string]any{
		"name": "foo", "question": "foo/bar",
	}); err == nil {
		t.Fatal("want error for missing value")
	}
}

func TestModuleDebconfInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDebconf(context.Background(), conn, map[string]any{
		"name": "foo", "question": "foo/bar", "value": "x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
