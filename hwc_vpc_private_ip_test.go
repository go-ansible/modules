package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcVpcPrivateIpCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListPrivateIPs --subnet_id=subnet-1": {RC: 0, Stdout: `{"privateips":[]}`},
		"hcloud VPC CreatePrivateIP '--privateips.[0].subnet_id=subnet-1'": {
			RC: 0, Stdout: `{"privateips":[{"id":"ip-1","subnet_id":"subnet-1","ip_address":"10.0.0.5"}]}`,
		},
	})
	res, err := moduleHwcVpcPrivateIp(context.Background(), conn, map[string]any{"subnet_id": "subnet-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "ip-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcVpcPrivateIpAmbiguousWithoutIPAddress(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListPrivateIPs --subnet_id=subnet-1": {
			RC: 0, Stdout: `{"privateips":[{"id":"ip-1","subnet_id":"subnet-1"},{"id":"ip-2","subnet_id":"subnet-1"}]}`,
		},
	})
	res, err := moduleHwcVpcPrivateIp(context.Background(), conn, map[string]any{"subnet_id": "subnet-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: ambiguous without ip_address")
	}
}
