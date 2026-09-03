package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulRoleCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl role read -http-addr=http://localhost:8500 -name foo-role -format=json": {RC: 1},
		"consul acl role create -http-addr=http://localhost:8500 -name foo-role -policy-name policy-1 -format=json": {
			RC:     0,
			Stdout: `{"ID":"r1","Name":"foo-role","Policies":[{"ID":"p1","Name":"policy-1"}]}`,
		},
	})
	res, err := moduleConsulRole(context.Background(), conn, map[string]any{
		"name":     "foo-role",
		"policies": []any{map[string]any{"name": "policy-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "create" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulRoleUnchangedWhenFieldOmitted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl role read -http-addr=http://localhost:8500 -name foo-role -format=json": {
			RC:     0,
			Stdout: `{"ID":"r1","Name":"foo-role","Description":"d","Policies":[{"ID":"p1","Name":"policy-1"}]}`,
		},
	})
	res, err := moduleConsulRole(context.Background(), conn, map[string]any{"name": "foo-role", "description": "d"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want only the read (policies omitted from args must not force an update)", conn.Commands)
	}
}

func TestModuleConsulRoleUpdatePoliciesChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl role read -http-addr=http://localhost:8500 -name foo-role -format=json": {
			RC:     0,
			Stdout: `{"ID":"r1","Name":"foo-role","Policies":[{"ID":"p1","Name":"policy-1"}]}`,
		},
		"consul acl role update -http-addr=http://localhost:8500 -id r1 -name foo-role -policy-name policy-2 -format=json": {
			RC:     0,
			Stdout: `{"ID":"r1","Name":"foo-role","Policies":[{"ID":"p2","Name":"policy-2"}]}`,
		},
	})
	res, err := moduleConsulRole(context.Background(), conn, map[string]any{
		"name":     "foo-role",
		"policies": []any{map[string]any{"name": "policy-2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "update" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulRoleClearPoliciesWithEmptyList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl role read -http-addr=http://localhost:8500 -name foo-role -format=json": {
			RC:     0,
			Stdout: `{"ID":"r1","Name":"foo-role","Policies":[{"ID":"p1","Name":"policy-1"}]}`,
		},
		"consul acl role update -http-addr=http://localhost:8500 -id r1 -name foo-role -format=json": {
			RC:     0,
			Stdout: `{"ID":"r1","Name":"foo-role","Policies":[]}`,
		},
	})
	res, err := moduleConsulRole(context.Background(), conn, map[string]any{
		"name":     "foo-role",
		"policies": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulRoleDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl role read -http-addr=http://localhost:8500 -name foo-role -format=json": {
			RC:     0,
			Stdout: `{"ID":"r1","Name":"foo-role"}`,
		},
		"consul acl role delete -http-addr=http://localhost:8500 -id r1": {RC: 0},
	})
	res, err := moduleConsulRole(context.Background(), conn, map[string]any{"name": "foo-role", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "delete" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulRoleTemplatedPoliciesUnsupported(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleConsulRole(context.Background(), conn, map[string]any{
		"name":               "foo-role",
		"templated_policies": []any{map[string]any{"template_name": "builtin/service"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for unsupported templated_policies")
	}
}

func TestModuleConsulRoleMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulRole(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
