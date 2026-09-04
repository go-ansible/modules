package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacketIpSubnetAssign(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v metal":                                   {RC: 0},
		"metal device get -i dev-1 -o json":                  {RC: 0, Stdout: `{"id":"dev-1","ip_addresses":[]}`},
		"metal ip assign -a 147.75.40.2/31 -d dev-1 -o json": {RC: 0},
	})
	args := map[string]any{"cidr": "147.75.40.2/31", "device_id": "dev-1"}
	res, err := modulePacketIpSubnet(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePacketIpSubnetAlreadyAssigned(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v metal": {RC: 0},
		"metal device get -i dev-1 -o json": {
			RC: 0, Stdout: `{"id":"dev-1","ip_addresses":[{"id":"assign-1","address":"147.75.40.2","cidr":31}]}`,
		},
	})
	args := map[string]any{"cidr": "147.75.40.2/31", "device_id": "dev-1"}
	res, err := modulePacketIpSubnet(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePacketIpSubnetAbsentNoDeviceFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v metal": {RC: 0}})
	args := map[string]any{"cidr": "147.75.40.2/31", "state": "absent"}
	res, err := modulePacketIpSubnet(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: no device_id/hostname for absent")
	}
}
