package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClientRolemappingAssign(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get clients/cid-1/roles -r myrealm": {{RC: 0,
			Stdout: `[{"id":"role-1","name":"role_name1"}]`}},
		"kcadm.sh get groups/gid-1/role-mappings/clients/cid-1 -r myrealm": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"role-1","name":"role_name1"}]`},
		},
		"kcadm.sh create groups/gid-1/role-mappings/clients/cid-1 -r myrealm -f -": {{RC: 0}},
	})
	res, err := moduleKeycloakClientRolemapping(context.Background(), conn, map[string]any{
		"realm": "myrealm", "cid": "cid-1", "gid": "gid-1", "state": "present",
		"roles": []any{map[string]any{"name": "role_name1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientRolemappingAlreadyMapped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients/cid-1/roles -r myrealm": {RC: 0,
			Stdout: `[{"id":"role-1","name":"role_name1"}]`},
		"kcadm.sh get groups/gid-1/role-mappings/clients/cid-1 -r myrealm": {RC: 0,
			Stdout: `[{"id":"role-1","name":"role_name1"}]`},
	})
	res, err := moduleKeycloakClientRolemapping(context.Background(), conn, map[string]any{
		"realm": "myrealm", "cid": "cid-1", "gid": "gid-1", "state": "present",
		"roles": []any{map[string]any{"id": "role-1", "name": "role_name1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientRolemappingUnassign(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get clients/cid-1/roles -r myrealm": {{RC: 0,
			Stdout: `[{"id":"role-1","name":"role_name1"}]`}},
		"kcadm.sh get groups/gid-1/role-mappings/clients/cid-1 -r myrealm": {
			{RC: 0, Stdout: `[{"id":"role-1","name":"role_name1"}]`},
			{RC: 0, Stdout: "[]"},
		},
		"kcadm.sh delete groups/gid-1/role-mappings/clients/cid-1 -r myrealm -f -": {{RC: 0}},
	})
	res, err := moduleKeycloakClientRolemapping(context.Background(), conn, map[string]any{
		"realm": "myrealm", "cid": "cid-1", "gid": "gid-1", "state": "absent",
		"roles": []any{map[string]any{"id": "role-1", "name": "role_name1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientRolemappingMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakClientRolemapping(context.Background(), conn, map[string]any{
		"gid": "gid-1",
	}); err == nil {
		t.Fatal("want error when neither cid nor client_id is given")
	}
}
