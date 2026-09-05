package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOvhIPLoadbalancingBackendLBMissingFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud":                             {RC: 0},
		"ovhcloud iploadbalancing get ip-1.1.1.1 -o json": {RC: 1, Stderr: "404"},
	})
	res, err := moduleOvhIPLoadbalancingBackend(context.Background(), conn, map[string]any{
		"name": "ip-1.1.1.1", "backend": "212.1.1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleOvhIPLoadbalancingBackendNoBackendVerbFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud":                             {RC: 0},
		"ovhcloud iploadbalancing get ip-1.1.1.1 -o json": {RC: 0, Stdout: `{"serviceName":"ip-1.1.1.1"}`},
	})
	res, err := moduleOvhIPLoadbalancingBackend(context.Background(), conn, map[string]any{
		"name": "ip-1.1.1.1", "backend": "212.1.1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (ovhcloud-cli has no backend command), res = %+v", res)
	}
}

func TestModuleOvhIPLoadbalancingBackendMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 1},
	})
	res, err := moduleOvhIPLoadbalancingBackend(context.Background(), conn, map[string]any{
		"name": "ip-1.1.1.1", "backend": "212.1.1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleOvhIPLoadbalancingBackendMissingArgs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 0},
	})
	_, err := moduleOvhIPLoadbalancingBackend(context.Background(), conn, map[string]any{"name": "ip-1.1.1.1"})
	if err == nil {
		t.Fatal("want error for missing backend")
	}
}
