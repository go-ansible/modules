package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcVpcEipCreateNoID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC CreatePublicip --publicip.type=5_bgp": {
			RC: 0, Stdout: `{"publicip":{"id":"eip-1","public_ip_address":"1.2.3.4"}}`,
		},
	})
	res, err := moduleHwcVpcEip(context.Background(), conn, map[string]any{"type": "5_bgp"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "eip-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcVpcEipIdempotentByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                           {RC: 0},
		"hcloud VPC ShowPublicip --publicip_id=eip-1": {RC: 0, Stdout: `{"publicip":{"id":"eip-1"}}`},
	})
	args := map[string]any{"type": "5_bgp", "id": "eip-1"}
	res, err := moduleHwcVpcEip(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHwcVpcEipAbsentNoID(t *testing.T) {
	res, err := moduleHwcVpcEip(context.Background(), newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
	}), map[string]any{"type": "5_bgp", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
