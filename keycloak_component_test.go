package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakComponentCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get components -r some_realm -q type=org.keycloak.storage.UserStorageProvider": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"comp-1","name":"my storage provider"}]`},
		},
		"kcadm.sh create components -r some_realm -f -": {{RC: 0}},
	})
	res, err := moduleKeycloakComponent(context.Background(), conn, map[string]any{
		"parent_id": "some_realm", "name": "my storage provider", "provider_id": "my storage",
		"provider_type": "org.keycloak.storage.UserStorageProvider",
		"config":        map[string]any{"cachePolicy": "NO_CACHE", "enabled": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakComponentInSync(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get components -r some_realm -q type=org.keycloak.storage.UserStorageProvider": {RC: 0,
			Stdout: `[{"id":"comp-1","name":"mykey","providerId":"my storage","providerType":"org.keycloak.storage.UserStorageProvider","parentId":"some_realm","config":{}}]`},
	})
	res, err := moduleKeycloakComponent(context.Background(), conn, map[string]any{
		"parent_id": "some_realm", "name": "mykey", "provider_id": "my storage",
		"provider_type": "org.keycloak.storage.UserStorageProvider",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakComponentDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get components -r some_realm -q type=org.keycloak.storage.UserStorageProvider": {RC: 0,
			Stdout: `[{"id":"comp-1","name":"mykey"}]`},
		"kcadm.sh delete components/comp-1 -r some_realm": {RC: 0},
	})
	res, err := moduleKeycloakComponent(context.Background(), conn, map[string]any{
		"parent_id": "some_realm", "name": "mykey", "provider_id": "my storage",
		"provider_type": "org.keycloak.storage.UserStorageProvider", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakComponentMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakComponent(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
