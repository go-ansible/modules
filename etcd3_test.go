package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleEtcd3Present(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ETCDCTL_API=3 etcdctl get foo --print-value-only --endpoints=http://localhost:2379": {RC: 1},
		"ETCDCTL_API=3 etcdctl put foo baz3 --endpoints=http://localhost:2379":               {RC: 0},
	})
	res, err := moduleEtcd3(context.Background(), conn, map[string]any{
		"key": "foo", "value": "baz3", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["old_value"] != "" {
		t.Fatalf("old_value = %v", res.Extra["old_value"])
	}
}

func TestModuleEtcd3PresentUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ETCDCTL_API=3 etcdctl get foo --print-value-only --endpoints=http://localhost:2379": {RC: 0, Stdout: "baz3\n"},
	})
	res, err := moduleEtcd3(context.Background(), conn, map[string]any{
		"key": "foo", "value": "baz3", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no put run", conn.Commands)
	}
}

func TestModuleEtcd3Absent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ETCDCTL_API=3 etcdctl get foo --print-value-only --endpoints=http://localhost:2379": {RC: 0, Stdout: "baz3\n"},
		"ETCDCTL_API=3 etcdctl del foo --endpoints=http://localhost:2379":                    {RC: 0},
	})
	res, err := moduleEtcd3(context.Background(), conn, map[string]any{
		"key": "foo", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["old_value"] != "baz3" {
		t.Fatalf("old_value = %v", res.Extra["old_value"])
	}
}

func TestModuleEtcd3TLSAndUser(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ETCDCTL_API=3 etcdctl get foo --print-value-only --endpoints=https://localhost:2379 --user=someone:password123 --cacert=/etc/ssl/certs/CA_CERT.pem": {RC: 1},
		"ETCDCTL_API=3 etcdctl put foo baz3 --endpoints=https://localhost:2379 --user=someone:password123 --cacert=/etc/ssl/certs/CA_CERT.pem":               {RC: 0},
	})
	res, err := moduleEtcd3(context.Background(), conn, map[string]any{
		"key": "foo", "value": "baz3", "state": "present",
		"user": "someone", "password": "password123", "ca_cert": "/etc/ssl/certs/CA_CERT.pem",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleEtcd3MissingKey(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleEtcd3(context.Background(), conn, map[string]any{"state": "present", "value": "x"})
	if err == nil {
		t.Fatal("want error for missing key")
	}
}
