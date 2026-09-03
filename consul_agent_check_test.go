package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulAgentCheckRegister(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul services register -http-addr=http://localhost:8500 /tmp/consul-agent-check.json": {RC: 0},
	})
	res, err := moduleConsulAgentCheck(context.Background(), conn, map[string]any{
		"name": "nginx_tcp_check", "service_id": "nginx", "tcp": "localhost:80", "interval": "60s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	want := `{"checks":[{"id":"nginx_tcp_check","interval":"60s","name":"nginx_tcp_check","serviceid":"nginx","tcp":"localhost:80"}]}`
	if conn.Stdins[0] != want {
		t.Fatalf("stdin = %q, want %q", conn.Stdins[0], want)
	}
}

func TestModuleConsulAgentCheckAlwaysChanged(t *testing.T) {
	// Real consul_agent_check's own doc says it always reports changed;
	// calling it twice in a row must Changed=true both times.
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul services register -http-addr=http://localhost:8500 /tmp/consul-agent-check.json": {RC: 0},
	})
	args := map[string]any{"name": "x", "ttl": "30s"}
	for i := 0; i < 2; i++ {
		res, err := moduleConsulAgentCheck(context.Background(), conn, args)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Changed {
			t.Fatalf("iteration %d: res = %+v, want always Changed", i, res)
		}
	}
}

func TestModuleConsulAgentCheckDeregister(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul services deregister -http-addr=http://localhost:8500 /tmp/consul-agent-check.json": {RC: 0},
	})
	res, err := moduleConsulAgentCheck(context.Background(), conn, map[string]any{
		"name": "nginx_http_check", "id": "nginx_http_check", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "delete" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAgentCheckMissingInterval(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulAgentCheck(context.Background(), conn, map[string]any{
		"name": "x", "http": "http://localhost/status",
	}); err == nil {
		t.Fatal("want error for missing interval")
	}
}

func TestModuleConsulAgentCheckMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulAgentCheck(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
