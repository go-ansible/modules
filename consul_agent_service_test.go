package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulAgentServiceCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul catalog service nginx -format=json -http-addr=http://localhost:8500":               {RC: 0, Stdout: "[]"},
		"consul services register -http-addr=http://localhost:8500 /tmp/consul-agent-service.json": {RC: 0},
	})
	res, err := moduleConsulAgentService(context.Background(), conn, map[string]any{
		"name": "nginx", "service_port": 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "create" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAgentServiceUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul catalog service nginx -format=json -http-addr=http://localhost:8500": {
			RC: 0,
			Stdout: `[{"ServiceID":"nginx","ServiceName":"nginx","ServiceAddress":"","ServicePort":80,` +
				`"ServiceTags":[],"ServiceMeta":{},"ServiceEnableTagOverride":false,` +
				`"ServiceWeights":{"Passing":1,"Warning":1}}]`,
		},
	})
	res, err := moduleConsulAgentService(context.Background(), conn, map[string]any{
		"name": "nginx", "service_port": 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAgentServiceUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul catalog service nginx -format=json -http-addr=http://localhost:8500": {
			RC: 0,
			Stdout: `[{"ServiceID":"nginx","ServiceName":"nginx","ServiceAddress":"","ServicePort":80,` +
				`"ServiceTags":[],"ServiceMeta":{},"ServiceEnableTagOverride":false,` +
				`"ServiceWeights":{"Passing":1,"Warning":1}}]`,
		},
		"consul services register -http-addr=http://localhost:8500 /tmp/consul-agent-service.json": {RC: 0},
	})
	res, err := moduleConsulAgentService(context.Background(), conn, map[string]any{
		"name": "nginx", "service_port": 8080,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "update" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAgentServiceDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul catalog service nginx -format=json -http-addr=http://localhost:8500": {
			RC:     0,
			Stdout: `[{"ServiceID":"nginx","ServiceName":"nginx"}]`,
		},
		"consul services deregister -http-addr=http://localhost:8500 -id nginx": {RC: 0},
	})
	res, err := moduleConsulAgentService(context.Background(), conn, map[string]any{
		"id": "nginx", "name": "nginx", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "delete" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAgentServiceDeleteAbsentNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul catalog service nginx -format=json -http-addr=http://localhost:8500": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleConsulAgentService(context.Background(), conn, map[string]any{
		"id": "nginx", "name": "nginx", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulAgentServiceMissingID(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulAgentService(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name/id")
	}
}
