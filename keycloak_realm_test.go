package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakRealmCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":         {RC: 0},
		"kcadm.sh get realms/myrealm": {RC: 1},
		"kcadm.sh create realms -f -": {RC: 0},
	})
	args := map[string]any{"realm": "myrealm", "enabled": true}
	res, err := moduleKeycloakRealm(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakRealmAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":      {RC: 0},
		"kcadm.sh get realms/gone": {RC: 1},
	})
	args := map[string]any{"realm": "gone", "state": "absent"}
	res, err := moduleKeycloakRealm(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKeycloakRealmDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get realms/old": {
			RC: 0, Stdout: `{"realm":"old","enabled":true}`,
		},
		"kcadm.sh delete realms/old": {RC: 0},
	})
	args := map[string]any{"realm": "old", "state": "absent"}
	res, err := moduleKeycloakRealm(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakRealmUpdateIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get realms/myrealm": {
			RC: 0, Stdout: `{"realm":"myrealm","enabled":true,"displayName":"My Realm"}`,
		},
	})
	args := map[string]any{"realm": "myrealm", "enabled": true, "display_name": "My Realm"}
	res, err := moduleKeycloakRealm(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}
