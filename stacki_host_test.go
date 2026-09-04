package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleStackiHostAddNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"stack list host": {RC: 0, Stdout: "backend-0-0\nbackend-0-1\n"},
		"stack add host test-1 rack=0 rank=0 appliance=backend": {RC: 0},
		"stack sync config":      {RC: 0},
		"stack sync host config": {RC: 0},
	})
	res, err := moduleStackiHost(context.Background(), conn, map[string]any{
		"name":            "test-1",
		"stacki_user":     "usr",
		"stacki_password": "pwd",
		"stacki_endpoint": "url",
		"prim_intf_mac":   "mac",
		"prim_intf_ip":    "1.2.3.4",
		"prim_intf":       "eth0",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Faithfully-reproduced real bug: changed is false even after adding.
	if res.Changed {
		t.Fatal("want changed=false after add, matching real stacki_host's own bug")
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "stack add host test-1 rack=0 rank=0 appliance=backend" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleStackiHostAddMissingRequired(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"stack list host": {RC: 0, Stdout: ""},
	})
	_, err := moduleStackiHost(context.Background(), conn, map[string]any{
		"name": "test-1",
	})
	if err == nil {
		t.Fatal("want error for missing prim_intf/prim_intf_ip/prim_intf_mac")
	}
}

func TestModuleStackiHostAlreadyExistsNoForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"stack list host": {RC: 0, Stdout: "test-1\n"},
	})
	res, err := moduleStackiHost(context.Background(), conn, map[string]any{"name": "test-1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	for _, c := range conn.Commands {
		if c == "stack add host test-1 rack=0 rank=0 appliance=backend" {
			t.Fatal("must not attempt to add an already-existing host")
		}
	}
}

func TestModuleStackiHostForceInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"stack list host": {RC: 0, Stdout: "test-1\n"},
		"stack set host boot test-1 action=install": {RC: 0},
		"stack sync config":                         {RC: 0},
		"stack sync host config":                    {RC: 0},
	})
	res, err := moduleStackiHost(context.Background(), conn, map[string]any{
		"name": "test-1", "force_install": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed=true for force_install")
	}
}

func TestModuleStackiHostRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"stack list host":          {RC: 0, Stdout: "test-1\n"},
		"stack remove host test-1": {RC: 0},
		"stack sync config":        {RC: 0},
		"stack sync host config":   {RC: 0},
	})
	res, err := moduleStackiHost(context.Background(), conn, map[string]any{
		"name": "test-1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed=true for remove")
	}
}

func TestModuleStackiHostRemoveAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"stack list host": {RC: 0, Stdout: ""},
	})
	res, err := moduleStackiHost(context.Background(), conn, map[string]any{
		"name": "test-1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleStackiHostValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleStackiHost(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleStackiHost(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}
