package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOvhIPFailoverAlreadyRouted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud":             {RC: 0},
		"ovhcloud ip get 1.1.1.1 -o json": {RC: 0, Stdout: `{"routedTo":{"serviceName":"ns666.ovh.net"}}`},
	})
	res, err := moduleOvhIPFailover(context.Background(), conn, map[string]any{
		"name": "1.1.1.1", "service": "ns666.ovh.net",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	if res.Failed {
		t.Fatalf("want not failed, res = %+v", res)
	}
}

func TestModuleOvhIPFailoverNeedsMoveFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud":             {RC: 0},
		"ovhcloud ip get 1.1.1.1 -o json": {RC: 0, Stdout: `{"routedTo":{"serviceName":"other.ovh.net"}}`},
	})
	res, err := moduleOvhIPFailover(context.Background(), conn, map[string]any{
		"name": "1.1.1.1", "service": "ns666.ovh.net",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (ovhcloud-cli cannot move an IP), res = %+v", res)
	}
}

func TestModuleOvhIPFailoverIPNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud":             {RC: 0},
		"ovhcloud ip get 1.1.1.1 -o json": {RC: 1, Stderr: "404"},
	})
	res, err := moduleOvhIPFailover(context.Background(), conn, map[string]any{
		"name": "1.1.1.1", "service": "ns666.ovh.net",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleOvhIPFailoverMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 1},
	})
	res, err := moduleOvhIPFailover(context.Background(), conn, map[string]any{
		"name": "1.1.1.1", "service": "ns666.ovh.net",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleOvhIPFailoverCredentialsWiredAsEnv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ovhcloud": {RC: 0},
		"OVH_ENDPOINT=ovh-eu OVH_APPLICATION_KEY=ak OVH_APPLICATION_SECRET=as OVH_CONSUMER_KEY=ck ovhcloud ip get 1.1.1.1 -o json": {RC: 0, Stdout: `{"routedTo":{"serviceName":"ns666.ovh.net"}}`},
	})
	res, err := moduleOvhIPFailover(context.Background(), conn, map[string]any{
		"name": "1.1.1.1", "service": "ns666.ovh.net",
		"endpoint": "ovh-eu", "application_key": "ak", "application_secret": "as", "consumer_key": "ck",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
