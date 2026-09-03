package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulPolicyCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl policy read -http-addr=http://localhost:8500 -name foo-access -format=json": {RC: 1, Stderr: "Error! Failed to read policy: unable to find"},
		"consul acl policy create -http-addr=http://localhost:8500 -name foo-access -description desc -rules " +
			shellQuote(`key "foo" {}`) + " -format=json": {
			RC:     0,
			Stdout: `{"ID":"abc-123","Name":"foo-access","Description":"desc","Rules":"key \"foo\" {}"}`,
		},
	})
	res, err := moduleConsulPolicy(context.Background(), conn, map[string]any{
		"name": "foo-access", "description": "desc", "rules": `key "foo" {}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["operation"] != "create" {
		t.Fatalf("operation = %v", res.Extra["operation"])
	}
	policy, ok := res.Extra["policy"].(map[string]any)
	if !ok || policy["ID"] != "abc-123" {
		t.Fatalf("policy = %#v", res.Extra["policy"])
	}
}

func TestModuleConsulPolicyUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl policy read -http-addr=http://localhost:8500 -name foo-access -format=json": {
			RC:     0,
			Stdout: `{"ID":"abc-123","Name":"foo-access","Description":"desc","Rules":""}`,
		},
	})
	res, err := moduleConsulPolicy(context.Background(), conn, map[string]any{"name": "foo-access", "description": "desc"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want only the read", conn.Commands)
	}
}

func TestModuleConsulPolicyUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl policy read -http-addr=http://localhost:8500 -name foo-access -format=json": {
			RC:     0,
			Stdout: `{"ID":"abc-123","Name":"foo-access","Description":"old","Rules":""}`,
		},
		"consul acl policy update -http-addr=http://localhost:8500 -id abc-123 -name foo-access -description new -format=json": {
			RC:     0,
			Stdout: `{"ID":"abc-123","Name":"foo-access","Description":"new","Rules":""}`,
		},
	})
	res, err := moduleConsulPolicy(context.Background(), conn, map[string]any{"name": "foo-access", "description": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "update" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulPolicyDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl policy read -http-addr=http://localhost:8500 -name foo-access -format=json": {
			RC:     0,
			Stdout: `{"ID":"abc-123","Name":"foo-access"}`,
		},
		"consul acl policy delete -http-addr=http://localhost:8500 -id abc-123": {RC: 0},
	})
	res, err := moduleConsulPolicy(context.Background(), conn, map[string]any{"name": "foo-access", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "delete" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulPolicyDeleteAbsentNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl policy read -http-addr=http://localhost:8500 -name foo-access -format=json": {RC: 1},
	})
	res, err := moduleConsulPolicy(context.Background(), conn, map[string]any{"name": "foo-access", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulPolicyMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulPolicy(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleConsulPolicyBadState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulPolicy(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}
