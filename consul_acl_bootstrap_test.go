package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulACLBootstrapSuccess(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl bootstrap -http-addr=http://localhost:8500 -format=json": {
			RC:     0,
			Stdout: `{"AccessorID":"834a5881","SecretID":"secret"}`,
		},
	})
	res, err := moduleConsulACLBootstrap(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	result, ok := res.Extra["result"].(map[string]any)
	if !ok || result["AccessorID"] != "834a5881" {
		t.Fatalf("result = %#v", res.Extra["result"])
	}
}

func TestModuleConsulACLBootstrapWithSecretFile(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl bootstrap -http-addr=http://localhost:8500 -format=json /tmp/consul-bootstrap-secret": {
			RC:     0,
			Stdout: `{"AccessorID":"1","SecretID":"22eaeed1-bdbd-4651-724e-42ae6c43e387"}`,
		},
	})
	res, err := moduleConsulACLBootstrap(context.Background(), conn, map[string]any{
		"bootstrap_secret": "22eaeed1-bdbd-4651-724e-42ae6c43e387",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Stdins) == 0 || conn.Stdins[0] != "22eaeed1-bdbd-4651-724e-42ae6c43e387" {
		t.Fatalf("stdins = %v", conn.Stdins)
	}
}

func TestModuleConsulACLBootstrapAlreadyDone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl bootstrap -http-addr=http://localhost:8500 -format=json": {
			RC:     1,
			Stderr: "Failed ACL bootstrapping: ACL bootstrap no longer allowed (already performed)",
		},
	})
	res, err := moduleConsulACLBootstrap(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, want unchanged-not-failed for an already-bootstrapped cluster", res)
	}
}

func TestModuleConsulACLBootstrapOtherFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl bootstrap -http-addr=http://localhost:8500 -format=json": {
			RC:     1,
			Stderr: "connection refused",
		},
	})
	res, err := moduleConsulACLBootstrap(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a genuine error")
	}
}

func TestModuleConsulACLBootstrapBadState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulACLBootstrap(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}
