package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClientRolescopeAddRealmRoles(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {{RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`}},
		"kcadm.sh get clients/cid-1 -r myrealm": {{RC: 0,
			Stdout: `{"id":"cid-1","clientId":"myclient","fullScopeAllowed":false}`}},
		"kcadm.sh get roles -r myrealm": {{RC: 0,
			Stdout: `[{"id":"role-1","name":"realm-role-admin"}]`}},
		"kcadm.sh get clients/cid-1/scope-mappings/realm -r myrealm": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"role-1","name":"realm-role-admin"}]`},
		},
		"kcadm.sh create clients/cid-1/scope-mappings/realm -r myrealm -f -": {{RC: 0}},
	})
	res, err := moduleKeycloakClientRolescope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "state": "present",
		"role_names": []any{"realm-role-admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientRolescopeFullScopeAllowedFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1 -r myrealm": {RC: 0,
			Stdout: `{"id":"cid-1","clientId":"myclient","fullScopeAllowed":true}`},
	})
	res, err := moduleKeycloakClientRolescope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "state": "present",
		"role_names": []any{"realm-role-admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleKeycloakClientRolescopeAlreadyUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		"kcadm.sh get clients/cid-1 -r myrealm": {RC: 0,
			Stdout: `{"id":"cid-1","clientId":"myclient","fullScopeAllowed":false}`},
		"kcadm.sh get roles -r myrealm": {RC: 0,
			Stdout: `[{"id":"role-1","name":"realm-role-admin"}]`},
		"kcadm.sh get clients/cid-1/scope-mappings/realm -r myrealm": {RC: 0,
			Stdout: `[{"id":"role-1","name":"realm-role-admin"}]`},
	})
	res, err := moduleKeycloakClientRolescope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "state": "present",
		"role_names": []any{"realm-role-admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientRolescopeMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakClientRolescope(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
