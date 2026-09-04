package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakUserRolemappingRealmRole(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                                     {RC: 0},
		"kcadm.sh get-roles --uid u1 -r myrealm":                  {RC: 0, Stdout: `[]`},
		"kcadm.sh add-roles --uid u1 --rolename role1 -r myrealm": {RC: 0},
	})
	args := map[string]any{
		"realm": "myrealm",
		"uid":   "u1",
		"roles": []any{map[string]any{"name": "role1"}},
	}
	res, err := moduleKeycloakUserRolemapping(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakUserRolemappingClientRole(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get users -r myrealm -q username=user1": {
			RC: 0, Stdout: `[{"id":"u1","username":"user1"}]`,
		},
		"kcadm.sh get clients -r myrealm -q clientId=client1": {
			RC: 0, Stdout: `[{"id":"cuuid1","clientId":"client1"}]`,
		},
		"kcadm.sh get-roles --uid u1 --cclientid cuuid1 -r myrealm":                  {RC: 0, Stdout: `[]`},
		"kcadm.sh add-roles --uid u1 --cclientid cuuid1 --rolename role1 -r myrealm": {RC: 0},
	})
	args := map[string]any{
		"realm":           "myrealm",
		"target_username": "user1",
		"client_id":       "client1",
		"roles":           []any{map[string]any{"name": "role1"}},
	}
	res, err := moduleKeycloakUserRolemapping(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
