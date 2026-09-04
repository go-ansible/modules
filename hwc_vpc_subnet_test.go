package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcVpcSubnetCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListSubnets --cidr=10.0.0.0/24 --name=my-subnet --vpc_id=vpc-1": {RC: 0, Stdout: `{"subnets":[]}`},
		"hcloud VPC CreateSubnet --subnet.cidr=10.0.0.0/24 --subnet.gateway_ip=10.0.0.1 --subnet.name=my-subnet --subnet.vpc_id=vpc-1": {
			RC: 0, Stdout: `{"subnet":{"id":"subnet-1","name":"my-subnet"}}`,
		},
	})
	args := map[string]any{"cidr": "10.0.0.0/24", "gateway_ip": "10.0.0.1", "name": "my-subnet", "vpc_id": "vpc-1"}
	res, err := moduleHwcVpcSubnet(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "subnet-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcVpcSubnetIdempotentByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                          {RC: 0},
		"hcloud VPC ShowSubnet --subnet_id=subnet-1": {RC: 0, Stdout: `{"subnet":{"id":"subnet-1","name":"my-subnet"}}`},
	})
	args := map[string]any{
		"cidr": "10.0.0.0/24", "gateway_ip": "10.0.0.1", "name": "my-subnet", "vpc_id": "vpc-1", "id": "subnet-1",
	}
	res, err := moduleHwcVpcSubnet(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHwcVpcSubnetAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListSubnets --cidr=10.0.0.0/24 --name=my-subnet --vpc_id=vpc-1": {
			RC: 0, Stdout: `{"subnets":[{"id":"subnet-1","name":"my-subnet","vpc_id":"vpc-1","cidr":"10.0.0.0/24"}]}`,
		},
		"hcloud VPC DeleteSubnet --subnet_id=subnet-1 --vpc_id=vpc-1": {RC: 0},
	})
	args := map[string]any{
		"cidr": "10.0.0.0/24", "gateway_ip": "10.0.0.1", "name": "my-subnet", "vpc_id": "vpc-1", "state": "absent",
	}
	res, err := moduleHwcVpcSubnet(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
