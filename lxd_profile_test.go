package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLxdProfileMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxdProfile(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleLxdProfileInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxdProfile(context.Background(), conn, map[string]any{
		"name": "x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleLxdProfileCreate(t *testing.T) {
	name := "macvlan"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/profiles/" + name:                 {RC: 1},
		"lxc profile create " + name:                          {RC: 0},
		"lxc profile set " + name + " description my profile": {RC: 0},
		"lxc profile set " + name + " limits.memory 4GB":      {RC: 0},
	})
	res, err := moduleLxdProfile(context.Background(), conn, map[string]any{
		"name":        name,
		"description": "my profile",
		"config":      map[string]any{"limits.memory": "4GB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleLxdProfileAlreadyExistsUnchanged(t *testing.T) {
	name := "macvlan"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/profiles/" + name: {RC: 0, Stdout: `{"name":"macvlan","description":"my profile","config":{"limits.memory":"4GB"},"devices":{}}`},
	})
	res, err := moduleLxdProfile(context.Background(), conn, map[string]any{
		"name":        name,
		"description": "my profile",
		"config":      map[string]any{"limits.memory": "4GB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLxdProfileConfigDrift(t *testing.T) {
	name := "macvlan"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/profiles/" + name:            {RC: 0, Stdout: `{"name":"macvlan","description":"","config":{"limits.memory":"2GB"},"devices":{}}`},
		"lxc profile set " + name + " limits.memory 4GB": {RC: 0},
	})
	res, err := moduleLxdProfile(context.Background(), conn, map[string]any{
		"name":   name,
		"config": map[string]any{"limits.memory": "4GB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleLxdProfileAbsentAlready(t *testing.T) {
	name := "gone"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/profiles/" + name: {RC: 1},
	})
	res, err := moduleLxdProfile(context.Background(), conn, map[string]any{
		"name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleLxdProfileAbsentDeletes(t *testing.T) {
	name := "macvlan"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/profiles/" + name: {RC: 0, Stdout: `{"name":"macvlan","description":"","config":{},"devices":{}}`},
		"lxc profile delete " + name:          {RC: 0},
	})
	res, err := moduleLxdProfile(context.Background(), conn, map[string]any{
		"name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
