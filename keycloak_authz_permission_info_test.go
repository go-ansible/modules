package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakAuthzPermissionInfoFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/policy/search -r myrealm -q name=ScopePermission": {RC: 0,
			Stdout: `{"id":"perm-1","name":"ScopePermission","type":"scope"}`},
	})
	res, err := moduleKeycloakAuthzPermissionInfo(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "ScopePermission",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	qs, ok := res.Extra["queried_state"].(map[string]any)
	if !ok || qs["id"] != "perm-1" {
		t.Fatalf("queried_state = %v", res.Extra["queried_state"])
	}
}

func TestModuleKeycloakAuthzPermissionInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/policy/search -r myrealm -q name=Nope": {RC: 1},
	})
	res, err := moduleKeycloakAuthzPermissionInfo(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "Nope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
