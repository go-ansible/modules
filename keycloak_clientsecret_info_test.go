package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClientsecretInfoByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients/cid-1/client-secret -r myrealm": {RC: 0,
			Stdout: `{"type":"secret","value":"cUGnX1EIeTtPPAkcyGMv0ncyqDPu68P1"}`},
	})
	res, err := moduleKeycloakClientsecretInfo(context.Background(), conn, map[string]any{
		"realm": "myrealm", "id": "cid-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	info, ok := res.Extra["clientsecret_info"].(map[string]any)
	if !ok || info["value"] != "cUGnX1EIeTtPPAkcyGMv0ncyqDPu68P1" {
		t.Fatalf("clientsecret_info = %v", res.Extra["clientsecret_info"])
	}
}

func TestModuleKeycloakClientsecretInfoByClientID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=myClientId": {RC: 0,
			Stdout: `[{"id":"cid-1","clientId":"myClientId"}]`},
		"kcadm.sh get clients/cid-1/client-secret -r myrealm": {RC: 0,
			Stdout: `{"type":"secret","value":"abc"}`},
	})
	res, err := moduleKeycloakClientsecretInfo(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "myClientId",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClientsecretInfoClientNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=nope": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleKeycloakClientsecretInfo(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "nope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleKeycloakClientsecretInfoMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakClientsecretInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing id/client_id")
	}
}
