package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcVpcSecurityGroupCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                          {RC: 0},
		"hcloud VPC ListSecurityGroups --name=my-sg": {RC: 0, Stdout: `{"security_groups":[]}`},
		"hcloud VPC CreateSecurityGroup --security_group.name=my-sg": {
			RC: 0, Stdout: `{"security_group":{"id":"sg-1","name":"my-sg"}}`,
		},
	})
	res, err := moduleHwcVpcSecurityGroup(context.Background(), conn, map[string]any{"name": "my-sg"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "sg-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcVpcSecurityGroupAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                          {RC: 0},
		"hcloud VPC ListSecurityGroups --name=my-sg": {RC: 0, Stdout: `{"security_groups":[]}`},
	})
	res, err := moduleHwcVpcSecurityGroup(context.Background(), conn, map[string]any{"name": "my-sg", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
