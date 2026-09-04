package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakUserprofileCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components -r myrealm -q parent=myrealm -q type=org.keycloak.userprofile.UserProfileProvider": {
			RC: 0, Stdout: `[]`,
		},
		"kcadm.sh create components -r myrealm -f -": {RC: 0},
	})
	args := map[string]any{"parent_id": "myrealm"}
	res, err := moduleKeycloakUserprofile(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakUserprofileAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components -r myrealm -q parent=myrealm -q type=org.keycloak.userprofile.UserProfileProvider": {
			RC: 0, Stdout: `[]`,
		},
	})
	args := map[string]any{"parent_id": "myrealm", "state": "absent"}
	res, err := moduleKeycloakUserprofile(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKeycloakUserprofileNormalizesSnakeCaseKeys(t *testing.T) {
	// The desired config uses snake_case keys throughout (as a real
	// playbook's own ansible-native option names would); this port
	// must normalize them to the camelCase Keycloak's own UPConfig JSON
	// document expects (see keycloak_userprofile.go's own doc comment)
	// before comparing against, or sending, the JSON-encoded document.
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components -r myrealm -q parent=myrealm -q type=org.keycloak.userprofile.UserProfileProvider": {
			RC: 0, Stdout: `[{"id":"up1","config":{}}]`,
		},
		"kcadm.sh update components/up1 -r myrealm -f -": {RC: 0},
	})
	args := map[string]any{
		"parent_id": "myrealm",
		"config": map[string]any{
			"kc_user_profile_config": []any{
				map[string]any{
					"attributes": []any{
						map[string]any{"name": "username", "display_name": "${username}"},
					},
				},
			},
		},
	}
	res, err := moduleKeycloakUserprofile(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
