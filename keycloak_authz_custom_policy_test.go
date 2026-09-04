package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakAuthzCustomPolicyCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/policy/search -r myrealm -q name=OnlyOwner": {RC: 1},
		"kcadm.sh create clients/cid-1/authz/resource-server/policy/script-policy.js -r myrealm -f -": {RC: 0},
	})
	res, err := moduleKeycloakAuthzCustomPolicy(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "OnlyOwner", "policy_type": "script-policy.js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthzCustomPolicyAlreadyExistsNeverUpdated(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/policy/search -r myrealm -q name=OnlyOwner": {RC: 0,
			Stdout: `{"id":"p1","name":"OnlyOwner","type":"other-policy.js"}`},
	})
	res, err := moduleKeycloakAuthzCustomPolicy(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "OnlyOwner", "policy_type": "script-policy.js",
	})
	if err != nil {
		t.Fatal(err)
	}
	// real module never updates an existing custom policy, even if policy_type differs.
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthzCustomPolicyDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/policy/search -r myrealm -q name=OnlyOwner": {RC: 0,
			Stdout: `{"id":"p1","name":"OnlyOwner","type":"script-policy.js"}`},
		"kcadm.sh delete clients/cid-1/authz/resource-server/policy/p1 -r myrealm": {RC: 0},
	})
	res, err := moduleKeycloakAuthzCustomPolicy(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "OnlyOwner", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
