package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakComponentInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components -r myrealm -q parent=myrealm": {
			RC: 0, Stdout: `[{"id":"c1","name":"myldap","providerId":"ldap"}]`,
		},
	})
	res, err := moduleKeycloakComponentInfo(context.Background(), conn, map[string]any{"realm": "myrealm"})
	if err != nil {
		t.Fatal(err)
	}
	comps, ok := res.Extra["components"].([]map[string]any)
	if !ok || len(comps) != 1 || comps[0]["name"] != "myldap" {
		t.Fatalf("components = %#v", res.Extra["components"])
	}
}

func TestModuleKeycloakComponentInfoFilters(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get components -r myrealm -q parent=p1 -q name=rsa-enc-generated -q type=org.keycloak.keys.KeyProvider": {
			RC: 0, Stdout: `[]`,
		},
	})
	args := map[string]any{
		"realm":         "myrealm",
		"parent_id":     "p1",
		"name":          "rsa-enc-generated",
		"provider_type": "org.keycloak.keys.KeyProvider",
	}
	res, err := moduleKeycloakComponentInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Extra["components"].([]map[string]any)) != 0 {
		t.Fatalf("expected no components, got %#v", res.Extra["components"])
	}
}
