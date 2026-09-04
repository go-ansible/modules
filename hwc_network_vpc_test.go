package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcNetworkVpcCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListVpcs --cidr=10.0.0.0/16 --name=my-vpc": {RC: 0, Stdout: `{"vpcs":[]}`},
		"hcloud VPC CreateVpc --vpc.cidr=10.0.0.0/16 --vpc.name=my-vpc": {
			RC: 0, Stdout: `{"vpc":{"id":"vpc-1","name":"my-vpc","cidr":"10.0.0.0/16"}}`,
		},
	})
	args := map[string]any{"name": "my-vpc", "cidr": "10.0.0.0/16"}
	res, err := moduleHwcNetworkVpc(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "vpc-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcNetworkVpcIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListVpcs --cidr=10.0.0.0/16 --name=my-vpc": {
			RC: 0, Stdout: `{"vpcs":[{"id":"vpc-1","name":"my-vpc","cidr":"10.0.0.0/16"}]}`,
		},
	})
	args := map[string]any{"name": "my-vpc", "cidr": "10.0.0.0/16"}
	res, err := moduleHwcNetworkVpc(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHwcNetworkVpcAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListVpcs --cidr=10.0.0.0/16 --name=my-vpc": {
			RC: 0, Stdout: `{"vpcs":[{"id":"vpc-1","name":"my-vpc","cidr":"10.0.0.0/16"}]}`,
		},
		"hcloud VPC DeleteVpc --vpc_id=vpc-1": {RC: 0},
	})
	args := map[string]any{"name": "my-vpc", "cidr": "10.0.0.0/16", "state": "absent"}
	res, err := moduleHwcNetworkVpc(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHwcNetworkVpcAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListVpcs --cidr=10.0.0.0/16 --name=my-vpc": {RC: 0, Stdout: `{"vpcs":[]}`},
	})
	args := map[string]any{"name": "my-vpc", "cidr": "10.0.0.0/16", "state": "absent"}
	res, err := moduleHwcNetworkVpc(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHwcNetworkVpcAmbiguous(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListVpcs --cidr=10.0.0.0/16 --name=my-vpc": {
			RC: 0, Stdout: `{"vpcs":[{"id":"vpc-1","name":"my-vpc","cidr":"10.0.0.0/16"},{"id":"vpc-2","name":"my-vpc","cidr":"10.0.0.0/16"}]}`,
		},
	})
	args := map[string]any{"name": "my-vpc", "cidr": "10.0.0.0/16"}
	res, err := moduleHwcNetworkVpc(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: ambiguous match")
	}
}

func TestModuleHwcNetworkVpcMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 1},
	})
	args := map[string]any{"name": "my-vpc", "cidr": "10.0.0.0/16"}
	res, err := moduleHwcNetworkVpc(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: hcloud missing")
	}
}
