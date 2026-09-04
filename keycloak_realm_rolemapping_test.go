package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakRealmRolemappingAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                                     {RC: 0},
		"kcadm.sh get-roles --gid g1 -r myrealm":                  {RC: 0, Stdout: `[]`},
		"kcadm.sh add-roles --gid g1 --rolename role1 -r myrealm": {RC: 0},
	})
	args := map[string]any{
		"realm": "myrealm",
		"gid":   "g1",
		"roles": []any{map[string]any{"name": "role1"}},
	}
	res, err := moduleKeycloakRealmRolemapping(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakRealmRolemappingAlreadyMapped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get-roles --gid g1 -r myrealm": {
			RC: 0, Stdout: `[{"id":"r1","name":"role1"}]`,
		},
	})
	args := map[string]any{
		"realm": "myrealm",
		"gid":   "g1",
		"roles": []any{map[string]any{"name": "role1"}},
	}
	res, err := moduleKeycloakRealmRolemapping(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKeycloakRealmRolemappingUnmap(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get-roles --gid g1 -r myrealm": {
			RC: 0, Stdout: `[{"id":"r1","name":"role1"}]`,
		},
		"kcadm.sh remove-roles --gid g1 --rolename role1 -r myrealm": {RC: 0},
	})
	args := map[string]any{
		"realm": "myrealm",
		"gid":   "g1",
		"roles": []any{map[string]any{"name": "role1"}},
		"state": "absent",
	}
	res, err := moduleKeycloakRealmRolemapping(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
