package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulAuthMethodCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl auth-method read -http-addr=http://localhost:8500 -name test -format=json": {RC: 1},
		"consul acl auth-method create -http-addr=http://localhost:8500 -name test -type jwt -config " +
			shellQuote(`{"jwt_validation_pubkeys":["k"]}`) + " -format=json": {
			RC:     0,
			Stdout: `{"Name":"test","Type":"jwt","Config":{"jwt_validation_pubkeys":["k"]}}`,
		},
	})
	res, err := moduleConsulAuthMethod(context.Background(), conn, map[string]any{
		"name": "test", "type": "jwt",
		"config": map[string]any{"jwt_validation_pubkeys": []any{"k"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "create" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAuthMethodUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl auth-method read -http-addr=http://localhost:8500 -name test -format=json": {
			RC:     0,
			Stdout: `{"Name":"test","Type":"jwt","Description":"d","Config":{"a":1}}`,
		},
	})
	res, err := moduleConsulAuthMethod(context.Background(), conn, map[string]any{
		"name": "test", "description": "d", "config": map[string]any{"a": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAuthMethodImmutableType(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl auth-method read -http-addr=http://localhost:8500 -name test -format=json": {
			RC:     0,
			Stdout: `{"Name":"test","Type":"jwt"}`,
		},
	})
	res, err := moduleConsulAuthMethod(context.Background(), conn, map[string]any{"name": "test", "type": "oidc"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for changing an immutable type")
	}
}

func TestModuleConsulAuthMethodDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl auth-method read -http-addr=http://localhost:8500 -name test -format=json": {
			RC:     0,
			Stdout: `{"Name":"test"}`,
		},
		"consul acl auth-method delete -http-addr=http://localhost:8500 -name test": {RC: 0},
	})
	res, err := moduleConsulAuthMethod(context.Background(), conn, map[string]any{"name": "test", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "delete" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAuthMethodMissingTypeOnCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl auth-method read -http-addr=http://localhost:8500 -name test -format=json": {RC: 1},
	})
	if _, err := moduleConsulAuthMethod(context.Background(), conn, map[string]any{"name": "test"}); err == nil {
		t.Fatal("want error for missing type on create")
	}
}

func TestModuleConsulAuthMethodMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulAuthMethod(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
