package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClientscopeCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get client-scopes -r myrealm": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"cs-1","name":"my-scope"}]`},
		},
		"kcadm.sh create client-scopes -r myrealm -f -": {{RC: 0}},
	})
	res, err := moduleKeycloakClientscope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "name": "my-scope", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientscopeNoChangesRequired(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get client-scopes -r myrealm": {RC: 0,
			Stdout: `[{"id":"cs-1","name":"my-scope","description":"d"}]`},
	})
	res, err := moduleKeycloakClientscope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "name": "my-scope", "description": "d", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientscopeDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get client-scopes -r myrealm": {RC: 0,
			Stdout: `[{"id":"cs-1","name":"my-scope"}]`},
		"kcadm.sh delete client-scopes/cs-1 -r myrealm": {RC: 0},
	})
	res, err := moduleKeycloakClientscope(context.Background(), conn, map[string]any{
		"realm": "myrealm", "name": "my-scope", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientscopeMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakClientscope(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
