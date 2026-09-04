package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakRealmInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get realms/master": {
			RC: 0, Stdout: `{"realm":"master","publicKey":"PKEY","notBefore":0}`,
		},
	})
	res, err := moduleKeycloakRealmInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	info := res.Extra["realm_info"].(map[string]any)
	if info["realm"] != "master" || info["public_key"] != "PKEY" {
		t.Fatalf("realm_info = %#v", info)
	}
}

func TestModuleKeycloakRealmInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":      {RC: 0},
		"kcadm.sh get realms/gone": {RC: 1, Stderr: "not found"},
	})
	res, err := moduleKeycloakRealmInfo(context.Background(), conn, map[string]any{"realm": "gone"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}
