package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAwallEnable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awall list":       {RC: 0, Stdout: "foo disabled\nbar enabled\n"},
		"awall enable foo": {RC: 0},
	})
	res, err := moduleAwall(context.Background(), conn, map[string]any{
		"name": []any{"foo", "bar"}, "state": "enabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAwallAlreadyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awall list": {RC: 0, Stdout: "foo enabled\nbar enabled\n"},
	})
	res, err := moduleAwall(context.Background(), conn, map[string]any{
		"name": []any{"foo", "bar"}, "state": "enabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAwallDisableAndActivate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awall list":             {RC: 0, Stdout: "foo enabled\n"},
		"awall disable foo":      {RC: 0},
		"awall activate --force": {RC: 0},
	})
	res, err := moduleAwall(context.Background(), conn, map[string]any{
		"name": []any{"foo"}, "state": "disabled", "activate": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAwallActivateOnly(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awall activate --force": {RC: 0},
	})
	res, err := moduleAwall(context.Background(), conn, map[string]any{"activate": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAwallMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAwall(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: at least one of name or activate required")
	}
}
