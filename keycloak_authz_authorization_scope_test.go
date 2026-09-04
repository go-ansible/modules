package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakAuthzAuthorizationScopeCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/scope/search -r myrealm -q name=file:delete": {RC: 1},
		"kcadm.sh create clients/cid-1/authz/resource-server/scope -r myrealm -f -":                    {RC: 0},
	})
	res, err := moduleKeycloakAuthzAuthorizationScope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "file:delete", "display_name": "File delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthzAuthorizationScopeUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1/authz/resource-server/scope/search -r myrealm -q name=file:delete": {RC: 0,
			Stdout: `{"id":"s1","name":"file:delete","displayName":"File delete"}`},
	})
	res, err := moduleKeycloakAuthzAuthorizationScope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "file:delete", "display_name": "File delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthzAuthorizationScopeInvalidClient(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleKeycloakAuthzAuthorizationScope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "name": "file:delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
