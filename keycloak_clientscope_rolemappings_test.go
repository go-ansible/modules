package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClientscopeRolemappingsAddRealmRoles(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get client-scopes/frontend-clientscope -r myrealm": {{RC: 1}},
		"kcadm.sh get client-scopes -r myrealm": {{RC: 0,
			Stdout: `[{"id":"cs-1","name":"frontend-clientscope"}]`}},
		"kcadm.sh get roles -r myrealm": {{RC: 0,
			Stdout: `[{"id":"role-1","name":"realm-role-admin"}]`}},
		"kcadm.sh get client-scopes/cs-1/scope-mappings/realm -r myrealm": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"role-1","name":"realm-role-admin"}]`},
		},
		"kcadm.sh create client-scopes/cs-1/scope-mappings/realm -r myrealm -f -": {{RC: 0}},
	})
	res, err := moduleKeycloakClientscopeRolemappings(context.Background(), conn, map[string]any{
		"realm": "myrealm", "clientscope_id": "frontend-clientscope", "state": "present",
		"role_names": []any{"realm-role-admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientscopeRolemappingsByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get client-scopes/cs-1 -r myrealm": {RC: 0, Stdout: `{"id":"cs-1","name":"frontend-clientscope"}`},
		"kcadm.sh get roles -r myrealm": {RC: 0,
			Stdout: `[{"id":"role-1","name":"realm-role-admin"}]`},
		"kcadm.sh get client-scopes/cs-1/scope-mappings/realm -r myrealm": {RC: 0,
			Stdout: `[{"id":"role-1","name":"realm-role-admin"}]`},
	})
	res, err := moduleKeycloakClientscopeRolemappings(context.Background(), conn, map[string]any{
		"realm": "myrealm", "clientscope_id": "cs-1", "state": "present",
		"role_names": []any{"realm-role-admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientscopeRolemappingsNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get client-scopes/nope -r myrealm": {RC: 1},
		"kcadm.sh get client-scopes -r myrealm":      {RC: 0, Stdout: "[]"},
	})
	res, err := moduleKeycloakClientscopeRolemappings(context.Background(), conn, map[string]any{
		"realm": "myrealm", "clientscope_id": "nope", "role_names": []any{"x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
