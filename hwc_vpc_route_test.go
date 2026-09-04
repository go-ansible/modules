package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcVpcRouteCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListVpcRoutes --destination=10.1.0.0/16 --nexthop=peer-1 --type=peering --vpc_id=vpc-1": {
			RC: 0, Stdout: `{"routes":[]}`,
		},
		"hcloud VPC CreateVpcRoute --route.destination=10.1.0.0/16 --route.nexthop=peer-1 --route.type=peering --route.vpc_id=vpc-1": {
			RC: 0, Stdout: `{"route":{"id":"route-1"}}`,
		},
	})
	args := map[string]any{"destination": "10.1.0.0/16", "next_hop": "peer-1", "vpc_id": "vpc-1"}
	res, err := moduleHwcVpcRoute(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "route-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcVpcRouteAbsentByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                            {RC: 0},
		"hcloud VPC ShowVpcRoute --route_id=route-1":   {RC: 0, Stdout: `{"route":{"id":"route-1"}}`},
		"hcloud VPC DeleteVpcRoute --route_id=route-1": {RC: 0},
	})
	args := map[string]any{
		"destination": "10.1.0.0/16", "next_hop": "peer-1", "vpc_id": "vpc-1", "id": "route-1", "state": "absent",
	}
	res, err := moduleHwcVpcRoute(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
