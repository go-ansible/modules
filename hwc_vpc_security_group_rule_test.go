package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcVpcSecurityGroupRuleCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ListSecurityGroupRules --direction=ingress --security_group_id=sg-1": {
			RC: 0, Stdout: `{"security_group_rules":[]}`,
		},
		"hcloud VPC CreateSecurityGroupRule --security_group_rule.direction=ingress --security_group_rule.security_group_id=sg-1": {
			RC: 0, Stdout: `{"security_group_rule":{"id":"rule-1"}}`,
		},
	})
	args := map[string]any{"direction": "ingress", "security_group_id": "sg-1"}
	res, err := moduleHwcVpcSecurityGroupRule(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "rule-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcVpcSecurityGroupRuleAbsentByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud VPC ShowSecurityGroupRule --security_group_rule_id=rule-1": {
			RC: 0, Stdout: `{"security_group_rule":{"id":"rule-1"}}`,
		},
		"hcloud VPC DeleteSecurityGroupRule --security_group_rule_id=rule-1": {RC: 0},
	})
	args := map[string]any{"direction": "ingress", "security_group_id": "sg-1", "id": "rule-1", "state": "absent"}
	res, err := moduleHwcVpcSecurityGroupRule(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
