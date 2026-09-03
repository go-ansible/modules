package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNomadTokenMissingHost(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleNomadToken(context.Background(), conn, map[string]any{
		"state": "present", "name": "dev",
	}); err == nil {
		t.Fatal("want error for missing host")
	}
}

func TestModuleNomadTokenBootstrap(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad acl bootstrap -json -address=https://localhost:4646": {RC: 0, Stdout: `{"AccessorID":"x","SecretID":"y"}`},
	})
	res, err := moduleNomadToken(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "present", "token_type": "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleNomadTokenBootstrapAlreadyDone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad acl bootstrap -json -address=https://localhost:4646": {RC: 1, Stderr: "ACL bootstrap already done"},
	})
	res, err := moduleNomadToken(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "present", "token_type": "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged/not failed, res = %+v", res)
	}
}

func TestModuleNomadTokenCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad acl token list -json -address=https://localhost:4646": {RC: 0, Stdout: `[]`},
		"nomad acl token create -name=Dev token -type=client -policy=readonly -global=false -json -address=https://localhost:4646": {
			RC: 0, Stdout: `{"AccessorID":"a1","Name":"Dev token"}`,
		},
	})
	res, err := moduleNomadToken(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "present", "name": "Dev token",
		"token_type": "client", "policies": []any{"readonly"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleNomadTokenAlreadyUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad acl token list -json -address=https://localhost:4646": {RC: 0, Stdout: `[{"AccessorID":"a1","Name":"Dev token","Type":"client","Global":false,"Policies":["readonly"]}]`},
	})
	res, err := moduleNomadToken(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "present", "name": "Dev token",
		"token_type": "client", "policies": []any{"readonly"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleNomadTokenAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad acl token list -json -address=https://localhost:4646": {RC: 0, Stdout: `[{"AccessorID":"a1","Name":"Dev token"}]`},
		"nomad acl token delete a1 -address=https://localhost:4646":  {RC: 0},
	})
	res, err := moduleNomadToken(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "absent", "name": "Dev token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleNomadTokenAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad acl token list -json -address=https://localhost:4646": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleNomadToken(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "absent", "name": "Dev token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}
