package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakGroupCreateTopLevel(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                              {RC: 0},
		"kcadm.sh get groups -r myrealm -q search=mygroup": {RC: 0, Stdout: `[]`},
		"kcadm.sh create groups -r myrealm -f - -i":        {RC: 0, Stdout: "newid123"},
	})
	args := map[string]any{"name": "mygroup", "realm": "myrealm"}
	res, err := moduleKeycloakGroup(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	end := res.Extra["end_state"].(map[string]any)
	if end["id"] != "newid123" {
		t.Fatalf("end_state = %#v", end)
	}
}

func TestModuleKeycloakGroupDeleteByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get groups/g1 -r myrealm": {
			RC: 0, Stdout: `{"id":"g1","name":"mygroup"}`,
		},
		"kcadm.sh delete groups/g1 -r myrealm": {RC: 0},
	})
	args := map[string]any{"id": "g1", "realm": "myrealm", "state": "absent"}
	res, err := moduleKeycloakGroup(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakGroupAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                           {RC: 0},
		"kcadm.sh get groups -r myrealm -q search=gone": {RC: 0, Stdout: `[]`},
	})
	args := map[string]any{"name": "gone", "realm": "myrealm", "state": "absent"}
	res, err := moduleKeycloakGroup(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}
