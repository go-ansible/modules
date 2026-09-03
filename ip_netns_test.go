package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIPNetnsCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ip netns list":      {RC: 0, Stdout: ""},
		"ip netns add mario": {RC: 0},
	})
	res, err := moduleIPNetns(context.Background(), conn, map[string]any{"name": "mario"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIPNetnsAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ip netns list": {RC: 0, Stdout: "mario (id: 0)\n"},
	})
	res, err := moduleIPNetns(context.Background(), conn, map[string]any{"name": "mario"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleIPNetnsDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ip netns list":      {RC: 0, Stdout: "luigi (id: 1)\n"},
		"ip netns del luigi": {RC: 0},
	})
	res, err := moduleIPNetns(context.Background(), conn, map[string]any{"name": "luigi", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIPNetnsDeleteAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ip netns list": {RC: 0, Stdout: ""},
	})
	res, err := moduleIPNetns(context.Background(), conn, map[string]any{"name": "luigi", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleIPNetnsMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIPNetns(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
