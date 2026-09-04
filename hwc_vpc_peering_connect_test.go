package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcVpcPeeringConnectCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListVpcPeerings --local_vpc_id=vpc-1 --name=peer1": {RC: 0, Stdout: `{"peerings":[]}`},
		"hcloud VPC CreateVpcPeering --peering.local_vpc_info.vpc_id=vpc-1 --peering.name=peer1 --peering.peer_vpc_info.vpc_id=vpc-2": {
			RC: 0, Stdout: `{"peering":{"id":"peer-1","name":"peer1"}}`,
		},
	})
	args := map[string]any{
		"name": "peer1", "local_vpc_id": "vpc-1",
		"peering_vpc": map[string]any{"vpc_id": "vpc-2"},
	}
	res, err := moduleHwcVpcPeeringConnect(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "peer-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcVpcPeeringConnectMissingPeeringVpc(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v hcloud": {RC: 0}})
	args := map[string]any{"name": "peer1", "local_vpc_id": "vpc-1"}
	_, err := moduleHwcVpcPeeringConnect(context.Background(), conn, args)
	if err == nil {
		t.Fatal("want error: missing peering_vpc")
	}
}
