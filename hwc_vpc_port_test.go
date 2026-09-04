package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcVpcPortCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                               {RC: 0},
		"hcloud VPC ListPorts --subnet_id=subnet-1":       {RC: 0, Stdout: `{"ports":[]}`},
		"hcloud VPC CreatePort --port.subnet_id=subnet-1": {RC: 0, Stdout: `{"port":{"id":"port-1"}}`},
	})
	res, err := moduleHwcVpcPort(context.Background(), conn, map[string]any{"subnet_id": "subnet-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "port-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcVpcPortAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                         {RC: 0},
		"hcloud VPC ListPorts --subnet_id=subnet-1": {RC: 0, Stdout: `{"ports":[{"id":"port-1","subnet_id":"subnet-1"}]}`},
		"hcloud VPC DeletePort --port_id=port-1":    {RC: 0},
	})
	args := map[string]any{"subnet_id": "subnet-1", "state": "absent"}
	res, err := moduleHwcVpcPort(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
