package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakAuthzPermissionCreateScope(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/policy/search -r myrealm -q name=ScopePermission": {RC: 1},
		"kcadm.sh get clients/cid-1/authz/resource-server/scope/search -r myrealm -q name=file:delete": {RC: 0,
			Stdout: `{"id":"scope-1","name":"file:delete"}`},
		"kcadm.sh get clients/cid-1/authz/resource-server/policy/search -r myrealm -q 'name=Default Policy'": {RC: 0,
			Stdout: `{"id":"policy-1","name":"Default Policy"}`},
		"kcadm.sh create clients/cid-1/authz/resource-server/permission/scope -r myrealm -f -": {RC: 0},
	})
	res, err := moduleKeycloakAuthzPermission(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "ScopePermission", "permission_type": "scope",
		"scopes": []any{"file:delete"}, "policies": []any{"Default Policy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthzPermissionScopeRequiresScopes(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleKeycloakAuthzPermission(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "ScopePermission", "permission_type": "scope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleKeycloakAuthzPermissionDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/policy/search -r myrealm -q name=ScopePermission": {RC: 0,
			Stdout: `{"id":"perm-1","name":"ScopePermission","type":"scope"}`},
		"kcadm.sh delete clients/cid-1/authz/resource-server/policy/perm-1 -r myrealm": {RC: 0},
	})
	res, err := moduleKeycloakAuthzPermission(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "ScopePermission", "permission_type": "scope",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
