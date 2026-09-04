package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClientsecretRegenerateByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh create clients/cid-1/client-secret -r myrealm": {RC: 0},
		"kcadm.sh get clients/cid-1/client-secret -r myrealm": {RC: 0,
			Stdout: `{"type":"secret","value":"NEWSECRET"}`},
	})
	res, err := moduleKeycloakClientsecretRegenerate(context.Background(), conn, map[string]any{
		"realm": "myrealm", "id": "cid-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	end, ok := res.Extra["end_state"].(map[string]any)
	if !ok || end["value"] != "NEWSECRET" {
		t.Fatalf("end_state = %v", res.Extra["end_state"])
	}
}

func TestModuleKeycloakClientsecretRegenerateClientNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get clients -r myrealm -q clientId=nope": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleKeycloakClientsecretRegenerate(context.Background(), conn, map[string]any{
		"realm": "myrealm", "client_id": "nope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleKeycloakClientsecretRegenerateMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakClientsecretRegenerate(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing id/client_id")
	}
}
