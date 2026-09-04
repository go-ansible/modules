package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakUserFederationCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components -r myrealm -q parent=myrealm -q type=org.keycloak.storage.UserStorageProvider -q name=my-ldap": {
			RC: 0, Stdout: `[]`,
		},
		"kcadm.sh create components -r myrealm -f - -i": {RC: 0, Stdout: "fed1"},
	})
	args := map[string]any{
		"realm":       "myrealm",
		"name":        "my-ldap",
		"provider_id": "ldap",
	}
	res, err := moduleKeycloakUserFederation(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	end := res.Extra["end_state"].(map[string]any)
	if end["id"] != "fed1" {
		t.Fatalf("end_state = %#v", end)
	}
}

func TestModuleKeycloakUserFederationDeleteByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components/fed1 -r myrealm": {
			RC: 0, Stdout: `{"id":"fed1","name":"my-ldap"}`,
		},
		"kcadm.sh delete components/fed1 -r myrealm": {RC: 0},
	})
	args := map[string]any{"realm": "myrealm", "id": "fed1", "state": "absent"}
	res, err := moduleKeycloakUserFederation(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
