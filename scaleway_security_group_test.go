package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewaySecurityGroupCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance security-group list zone=fr-par-1 -o json": {RC: 0, Stdout: `[]`},
		"scw instance security-group create name=sg1 description=desc stateful=false inbound-default-policy=accept outbound-default-policy=accept project-id=proj1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"sg1","name":"sg1"}`,
		},
	})
	res, err := moduleScalewaySecurityGroup(context.Background(), conn, map[string]any{
		"name": "sg1", "description": "desc", "region": "par1", "project": "proj1",
		"stateful": false, "inbound_default_policy": "accept", "outbound_default_policy": "accept",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewaySecurityGroupFoundNoUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance security-group list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"sg1","name":"sg1","description":"totally different"}]`,
		},
	})
	res, err := moduleScalewaySecurityGroup(context.Background(), conn, map[string]any{
		"name": "sg1", "description": "desc", "region": "par1", "project": "proj1", "stateful": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v (real scaleway_security_group never updates an existing match)", res)
	}
}

func TestModuleScalewaySecurityGroupStatefulRequiresPolicies(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v scw": {RC: 0}})
	_, err := moduleScalewaySecurityGroup(context.Background(), conn, map[string]any{
		"name": "sg1", "region": "par1", "project": "proj1", "stateful": true,
	})
	if err == nil {
		t.Fatal("expected an error when stateful=true without inbound/outbound_default_policy")
	}
}

func TestModuleScalewaySecurityGroupDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance security-group list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"sg1","name":"sg1"}]`,
		},
		"scw instance security-group delete security-group-id=sg1 zone=fr-par-1": {RC: 0},
	})
	res, err := moduleScalewaySecurityGroup(context.Background(), conn, map[string]any{
		"name": "sg1", "region": "par1", "project": "proj1", "stateful": false, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
