package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClientCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myclient": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"cid-1","clientId":"myclient"}]`},
		},
		"kcadm.sh create clients -r myrealm -f -": {{RC: 0}},
		"kcadm.sh get clients/cid-1 -r myrealm": {
			{RC: 0, Stdout: `{"id":"cid-1","clientId":"myclient","protocol":"openid-connect"}`},
		},
	})
	res, err := moduleKeycloakClient(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myclient", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientNoChangesRequired(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients/cid-1 -r myrealm": {RC: 0,
			Stdout: `{"id":"cid-1","clientId":"myclient","enabled":true}`},
	})
	res, err := moduleKeycloakClient(context.Background(), conn, map[string]any{
		"realm": "myrealm", "id": "cid-1", "enabled": true, "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients/cid-1 -r myrealm": {RC: 0,
			Stdout: `{"id":"cid-1","clientId":"myclient","enabled":false}`},
		"kcadm.sh update clients/cid-1 -r myrealm -f -": {RC: 0},
	})
	res, err := moduleKeycloakClient(context.Background(), conn, map[string]any{
		"realm": "myrealm", "id": "cid-1", "enabled": true, "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients/cid-1 -r myrealm":    {RC: 0, Stdout: `{"id":"cid-1","clientId":"myclient"}`},
		"kcadm.sh delete clients/cid-1 -r myrealm": {RC: 0},
	})
	res, err := moduleKeycloakClient(context.Background(), conn, map[string]any{
		"realm": "myrealm", "id": "cid-1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakClient(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
