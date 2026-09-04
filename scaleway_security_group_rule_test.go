package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewaySecurityGroupRuleCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance security-group list-rules security-group-id=sg1 zone=fr-par-1 -o json": {RC: 0, Stdout: `[]`},
		"scw instance security-group create-rule security-group-id=sg1 protocol=TCP direction=inbound action=accept ip-range=0.0.0.0/0 dest-port-from=80 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"rule1","dest_port_from":80,"protocol":"TCP"}`,
		},
	})
	res, err := moduleScalewaySecurityGroupRule(context.Background(), conn, map[string]any{
		"region": "par1", "protocol": "TCP", "port": 80, "direction": "inbound", "action": "accept", "security_group": "sg1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewaySecurityGroupRuleAllPortsMatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance security-group list-rules security-group-id=sg1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"rule1","ip_range":"0.0.0.0/0","dest_port_from":null,"direction":"inbound","action":"accept","protocol":"TCP"}]`,
		},
	})
	res, err := moduleScalewaySecurityGroupRule(context.Background(), conn, map[string]any{
		"region": "par1", "protocol": "TCP", "port": nil, "direction": "inbound", "action": "accept", "security_group": "sg1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewaySecurityGroupRuleMissingPortKey(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v scw": {RC: 0}})
	_, err := moduleScalewaySecurityGroupRule(context.Background(), conn, map[string]any{
		"region": "par1", "protocol": "TCP", "direction": "inbound", "action": "accept", "security_group": "sg1",
	})
	if err == nil {
		t.Fatal("expected an error when port key is entirely absent")
	}
}

func TestModuleScalewaySecurityGroupRuleDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance security-group list-rules security-group-id=sg1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"rule1","ip_range":"0.0.0.0/0","dest_port_from":80,"direction":"inbound","action":"accept","protocol":"TCP"}]`,
		},
		"scw instance security-group delete-rule security-group-id=sg1 security-group-rule-id=rule1 zone=fr-par-1": {RC: 0},
	})
	res, err := moduleScalewaySecurityGroupRule(context.Background(), conn, map[string]any{
		"region": "par1", "protocol": "TCP", "port": 80, "direction": "inbound", "action": "accept",
		"security_group": "sg1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
