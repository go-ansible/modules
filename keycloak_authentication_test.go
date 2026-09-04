package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakAuthenticationCreateEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/flows -r myrealm":            {RC: 0, Stdout: "[]"},
		"kcadm.sh create authentication/flows -r myrealm -f - -i": {RC: 0, Stdout: "abc-123"},
		"kcadm.sh get authentication/flows/abc-123 -r myrealm": {RC: 0,
			Stdout: `{"id":"abc-123","alias":"myflow","providerId":"basic-flow"}`},
	})
	res, err := moduleKeycloakAuthentication(context.Background(), conn, map[string]any{
		"realm": "myrealm", "alias": "myflow", "providerId": "basic-flow", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationAlreadyExistsNoForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/flows -r myrealm": {RC: 0,
			Stdout: `[{"id":"abc-123","alias":"myflow","providerId":"basic-flow"}]`},
	})
	res, err := moduleKeycloakAuthentication(context.Background(), conn, map[string]any{
		"realm": "myrealm", "alias": "myflow", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/flows -r myrealm": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleKeycloakAuthentication(context.Background(), conn, map[string]any{
		"realm": "myrealm", "alias": "myflow", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakAuthentication(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
