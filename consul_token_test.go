package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulTokenCreateNoAccessorID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl token create -http-addr=http://localhost:8500 -description Testing -format=json": {
			RC:     0,
			Stdout: `{"AccessorID":"07a7de84","SecretID":"bd380fba","Description":"Testing"}`,
		},
	})
	res, err := moduleConsulToken(context.Background(), conn, map[string]any{"description": "Testing"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "create" {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no read when accessor_id is unset", conn.Commands)
	}
}

func TestModuleConsulTokenUpdateByAccessorID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl token read -http-addr=http://localhost:8500 -id 07a7de84 -format=json": {
			RC:     0,
			Stdout: `{"AccessorID":"07a7de84","Description":"old"}`,
		},
		"consul acl token update -http-addr=http://localhost:8500 -id 07a7de84 -description new -format=json": {
			RC:     0,
			Stdout: `{"AccessorID":"07a7de84","Description":"new"}`,
		},
	})
	res, err := moduleConsulToken(context.Background(), conn, map[string]any{
		"accessor_id": "07a7de84", "description": "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "update" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulTokenUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl token read -http-addr=http://localhost:8500 -id 07a7de84 -format=json": {
			RC:     0,
			Stdout: `{"AccessorID":"07a7de84","Description":"same"}`,
		},
	})
	res, err := moduleConsulToken(context.Background(), conn, map[string]any{
		"accessor_id": "07a7de84", "description": "same",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulTokenDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl token read -http-addr=http://localhost:8500 -id 07a7de84 -format=json": {
			RC:     0,
			Stdout: `{"AccessorID":"07a7de84"}`,
		},
		"consul acl token delete -http-addr=http://localhost:8500 -id 07a7de84": {RC: 0},
	})
	res, err := moduleConsulToken(context.Background(), conn, map[string]any{
		"accessor_id": "07a7de84", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "delete" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulTokenDeleteAbsentNoAccessorID(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleConsulToken(context.Background(), conn, map[string]any{"state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("commands = %v, want none when there's no accessor_id to delete", conn.Commands)
	}
}

func TestModuleConsulTokenTemplatedPoliciesUnsupported(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleConsulToken(context.Background(), conn, map[string]any{
		"templated_policies": []any{map[string]any{"template_name": "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for unsupported templated_policies")
	}
}

func TestModuleConsulTokenBadState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulToken(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}
