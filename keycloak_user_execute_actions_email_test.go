package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakUserExecuteActionsEmailByUsername(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get users -r master -q username=johndoe": {
			RC: 0, Stdout: `[{"id":"u1","username":"johndoe"}]`,
		},
		"kcadm.sh update users/u1/execute-actions-email -r master -f -": {RC: 0},
	})
	res, err := moduleKeycloakUserExecuteActionsEmail(context.Background(), conn, map[string]any{"username": "johndoe"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed (this module always reports changed)")
	}
	if res.Extra["user_id"] != "u1" {
		t.Fatalf("user_id = %#v", res.Extra["user_id"])
	}
	actions := res.Extra["actions"].([]string)
	if len(actions) != 1 || actions[0] != "UPDATE_PASSWORD" {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestModuleKeycloakUserExecuteActionsEmailByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh update users/9d59aa76/execute-actions-email -r MyRealm -q client_id=my-frontend -f -": {RC: 0},
	})
	args := map[string]any{
		"id":        "9d59aa76",
		"realm":     "MyRealm",
		"client_id": "my-frontend",
		"actions":   []any{"UPDATE_PASSWORD", "VERIFY_EMAIL"},
	}
	res, err := moduleKeycloakUserExecuteActionsEmail(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakUserExecuteActionsEmailMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	args := map[string]any{"id": "x", "username": "y"}
	_, err := moduleKeycloakUserExecuteActionsEmail(context.Background(), conn, args)
	if err == nil {
		t.Fatal("want error")
	}
}
