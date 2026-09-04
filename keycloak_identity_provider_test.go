package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakIdentityProviderCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get identity-provider/instances/oidc-idp -r myrealm": {RC: 1},
		"kcadm.sh create identity-provider/instances -r myrealm -f -":  {RC: 0},
	})
	args := map[string]any{
		"alias":       "oidc-idp",
		"realm":       "myrealm",
		"provider_id": "oidc",
		"enabled":     true,
	}
	res, err := moduleKeycloakIdentityProvider(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakIdentityProviderAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get identity-provider/instances/gone-idp -r myrealm": {RC: 1},
	})
	args := map[string]any{"alias": "gone-idp", "realm": "myrealm", "state": "absent"}
	res, err := moduleKeycloakIdentityProvider(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKeycloakIdentityProviderDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get identity-provider/instances/old-idp -r myrealm": {
			RC: 0, Stdout: `{"alias":"old-idp","providerId":"oidc"}`,
		},
		"kcadm.sh delete identity-provider/instances/old-idp -r myrealm": {RC: 0},
	})
	args := map[string]any{"alias": "old-idp", "realm": "myrealm", "state": "absent"}
	res, err := moduleKeycloakIdentityProvider(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
