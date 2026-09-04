package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClientscopeTypeRealmLevelAssign(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get default-default-client-scopes -r myrealm": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"cs-1","name":"profile"}]`},
		},
		"kcadm.sh get client-scopes -r myrealm": {{RC: 0,
			Stdout: `[{"id":"cs-1","name":"profile"}]`}},
		"kcadm.sh update default-default-client-scopes/cs-1 -r myrealm": {{RC: 0}},
	})
	res, err := moduleKeycloakClientscopeType(context.Background(), conn, map[string]any{
		"realm": "myrealm", "default_clientscopes": []any{"profile"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientscopeTypeClientLevel(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=MyCustomClient": {{RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"MyCustomClient"}]`}},
		"kcadm.sh get clients/cid-1/default-client-scopes -r myrealm": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"cs-1","name":"profile"}]`},
		},
		"kcadm.sh get client-scopes -r myrealm": {{RC: 0,
			Stdout: `[{"id":"cs-1","name":"profile"}]`}},
		"kcadm.sh update clients/cid-1/default-client-scopes/cs-1 -r myrealm": {{RC: 0}},
	})
	res, err := moduleKeycloakClientscopeType(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "MyCustomClient", "default_clientscopes": []any{"profile"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientscopeTypeUnassignExtra(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get default-default-client-scopes -r myrealm": {
			{RC: 0, Stdout: `[{"id":"cs-1","name":"profile"},{"id":"cs-2","name":"roles"}]`},
			{RC: 0, Stdout: `[{"id":"cs-1","name":"profile"}]`},
		},
		"kcadm.sh get client-scopes -r myrealm": {{RC: 0,
			Stdout: `[{"id":"cs-1","name":"profile"},{"id":"cs-2","name":"roles"}]`}},
		"kcadm.sh delete default-default-client-scopes/cs-2 -r myrealm": {{RC: 0}},
	})
	res, err := moduleKeycloakClientscopeType(context.Background(), conn, map[string]any{
		"realm": "myrealm", "default_clientscopes": []any{"profile"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientscopeTypeNoOp(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleKeycloakClientscopeType(context.Background(), conn, map[string]any{
		"realm": "myrealm",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
