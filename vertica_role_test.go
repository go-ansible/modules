package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleVerticaRoleCreate(t *testing.T) {
	factsQuery := "select name, assigned_roles from roles where name ilike " + verticaQuoteLiteral("myrole")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery):           {RC: 0, Stdout: ""},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("create role myrole"): {RC: 0},
	})
	res, err := moduleVerticaRole(context.Background(), conn, map[string]any{"role": "myrole"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVerticaRoleCreateWithAssignedRoles(t *testing.T) {
	factsQuery := "select name, assigned_roles from roles where name ilike " + verticaQuoteLiteral("myrole")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery):                   {RC: 0, Stdout: ""},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("create role myrole"):         {RC: 0},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("grant other_role to myrole"): {RC: 0},
	})
	res, err := moduleVerticaRole(context.Background(), conn, map[string]any{
		"role": "myrole", "assigned_roles": "other_role",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleVerticaRoleAlreadyExists(t *testing.T) {
	factsQuery := "select name, assigned_roles from roles where name ilike " + verticaQuoteLiteral("myrole")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery): {RC: 0, Stdout: "myrole|other_role\n"},
	})
	res, err := moduleVerticaRole(context.Background(), conn, map[string]any{
		"role": "myrole", "assigned_roles": "other_role",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleVerticaRoleAbsentAlreadyGone(t *testing.T) {
	factsQuery := "select name, assigned_roles from roles where name ilike " + verticaQuoteLiteral("myrole")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery): {RC: 0, Stdout: ""},
	})
	res, err := moduleVerticaRole(context.Background(), conn, map[string]any{"role": "myrole", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleVerticaRoleAbsentDrops(t *testing.T) {
	factsQuery := "select name, assigned_roles from roles where name ilike " + verticaQuoteLiteral("myrole")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery):                      {RC: 0, Stdout: "myrole|other_role\n"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("revoke other_role from myrole"): {RC: 0},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("drop role myrole cascade"):      {RC: 0},
	})
	res, err := moduleVerticaRole(context.Background(), conn, map[string]any{"role": "myrole", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleVerticaRoleMissingRole(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleVerticaRole(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing role")
	}
}

func TestModuleVerticaRoleNameAlias(t *testing.T) {
	factsQuery := "select name, assigned_roles from roles where name ilike " + verticaQuoteLiteral("myrole")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(factsQuery):           {RC: 0, Stdout: ""},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("create role myrole"): {RC: 0},
	})
	res, err := moduleVerticaRole(context.Background(), conn, map[string]any{"name": "myrole"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
