package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakRealmKeyCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components -r myrealm -q parent=myrealm -q type=org.keycloak.keys.KeyProvider -q name=custom": {
			RC: 0, Stdout: `[]`,
		},
		"kcadm.sh create components -r myrealm -f - -i": {RC: 0, Stdout: "newkey1"},
	})
	args := map[string]any{
		"name":        "custom",
		"parent_id":   "myrealm",
		"provider_id": "rsa",
		"config": map[string]any{
			"priority":  120,
			"active":    true,
			"enabled":   true,
			"algorithm": "RS256",
		},
	}
	res, err := moduleKeycloakRealmKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	end := res.Extra["end_state"].(map[string]any)
	if end["id"] != "newkey1" {
		t.Fatalf("end_state = %#v", end)
	}
}

func TestModuleKeycloakRealmKeyDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components -r myrealm -q parent=myrealm -q type=org.keycloak.keys.KeyProvider -q name=hmac-generated": {
			RC: 0, Stdout: `[{"id":"k1","name":"hmac-generated"}]`,
		},
		"kcadm.sh delete components/k1 -r myrealm": {RC: 0},
	})
	args := map[string]any{
		"name":      "hmac-generated",
		"parent_id": "myrealm",
		"state":     "absent",
	}
	res, err := moduleKeycloakRealmKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
