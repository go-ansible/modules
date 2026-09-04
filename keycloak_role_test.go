package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakRoleCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                             {RC: 0},
		"kcadm.sh get roles/myrole -r myrealm":            {RC: 1},
		"kcadm.sh create roles -r myrealm -s name=myrole": {RC: 0},
	})
	args := map[string]any{"name": "myrole", "realm": "myrealm"}
	res, err := moduleKeycloakRole(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakRoleAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                {RC: 0},
		"kcadm.sh get roles/gone -r myrealm": {RC: 1},
	})
	args := map[string]any{"name": "gone", "realm": "myrealm", "state": "absent"}
	res, err := moduleKeycloakRole(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKeycloakRoleDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                  {RC: 0},
		"kcadm.sh get roles/old -r myrealm":    {RC: 0, Stdout: `{"id":"r1","name":"old"}`},
		"kcadm.sh delete roles/old -r myrealm": {RC: 0},
	})
	args := map[string]any{"name": "old", "realm": "myrealm", "state": "absent"}
	res, err := moduleKeycloakRole(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakClientRoleCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {
			RC: 0, Stdout: `[{"id":"cuuid1","clientId":"myclient"}]`,
		},
		"kcadm.sh get clients/cuuid1/roles/myrole -r myrealm":            {RC: 1},
		"kcadm.sh create clients/cuuid1/roles -r myrealm -s name=myrole": {RC: 0},
	})
	args := map[string]any{"name": "myrole", "realm": "myrealm", "client_id": "myclient"}
	res, err := moduleKeycloakRole(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
