package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulRegisterServiceOnly(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul services register -http-addr=http://localhost:8500 /tmp/consul-service.json": {RC: 0},
	})
	res, err := moduleConsul(context.Background(), conn, map[string]any{
		"service_name": "nginx", "service_port": 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	want := `{"service":{"name":"nginx","port":80}}`
	if len(conn.Stdins) == 0 || conn.Stdins[0] != want {
		t.Fatalf("stdin = %q, want %q", conn.Stdins, want)
	}
}

func TestModuleConsulRegisterServiceWithCheck(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul services register -http-addr=http://localhost:8500 /tmp/consul-service.json": {RC: 0},
	})
	res, err := moduleConsul(context.Background(), conn, map[string]any{
		"service_name": "nginx", "service_port": 80, "script": "curl http://localhost", "interval": "60s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	want := `{"service":{"checks":[{"args":["curl","http://localhost"],"interval":"60s"}],"name":"nginx","port":80}}`
	if conn.Stdins[0] != want {
		t.Fatalf("stdin = %q, want %q", conn.Stdins[0], want)
	}
}

func TestModuleConsulRegisterStandaloneCheck(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul services register -http-addr=http://localhost:8500 /tmp/consul-service.json": {RC: 0},
	})
	res, err := moduleConsul(context.Background(), conn, map[string]any{
		"check_name": "Disk usage", "check_id": "disk_usage", "script": "/opt/disk_usage.py", "interval": "5m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	want := `{"checks":[{"args":["/opt/disk_usage.py"],"id":"disk_usage","interval":"5m","name":"Disk usage"}]}`
	if conn.Stdins[0] != want {
		t.Fatalf("stdin = %q, want %q", conn.Stdins[0], want)
	}
}

func TestModuleConsulDeregister(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul services deregister -http-addr=http://localhost:8500 -id nginx": {RC: 0},
	})
	res, err := moduleConsul(context.Background(), conn, map[string]any{"service_name": "nginx", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulDeregisterStandaloneCheckUnsupported(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleConsul(context.Background(), conn, map[string]any{"check_name": "x", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for deregistering a standalone check")
	}
}

func TestModuleConsulMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsul(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when nothing to register")
	}
}

func TestModuleConsulMissingInterval(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsul(context.Background(), conn, map[string]any{
		"service_name": "nginx", "tcp": "localhost:80",
	}); err == nil {
		t.Fatal("want error for missing interval with tcp check")
	}
}
