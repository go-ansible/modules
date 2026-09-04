package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakUserCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                             {RC: 0},
		"kcadm.sh get users -r myrealm -q username=user1": {RC: 0, Stdout: `[]`},
		"kcadm.sh create users -r myrealm -f - -i":        {RC: 0, Stdout: "u123"},
	})
	args := map[string]any{
		"username": "user1",
		"realm":    "myrealm",
		"email":    "user1@example.com",
		"enabled":  true,
	}
	res, err := moduleKeycloakUser(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["user_created"] != true {
		t.Fatalf("user_created = %#v", res.Extra["user_created"])
	}
}

func TestModuleKeycloakUserAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh":                            {RC: 0},
		"kcadm.sh get users -r myrealm -q username=gone": {RC: 0, Stdout: `[]`},
	})
	args := map[string]any{"username": "gone", "realm": "myrealm", "state": "absent"}
	res, err := moduleKeycloakUser(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKeycloakUserDeleteByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get users/u1 -r myrealm": {
			RC: 0, Stdout: `{"id":"u1","username":"user1"}`,
		},
		"kcadm.sh delete users/u1 -r myrealm": {RC: 0},
	})
	args := map[string]any{"id": "u1", "realm": "myrealm", "state": "absent"}
	res, err := moduleKeycloakUser(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKeycloakUserSetPasswordCredential(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get users -r myrealm -q username=user1": {
			RC: 0, Stdout: `[{"id":"u1","username":"user1"}]`,
		},
		"kcadm.sh get users/u1 -r myrealm": {
			RC: 0, Stdout: `{"id":"u1","username":"user1"}`,
		},
		"kcadm.sh update users/u1 -r myrealm -f -":                {RC: 0},
		"kcadm.sh update users/u1/reset-password -r myrealm -f -": {RC: 0},
	})
	args := map[string]any{
		"username": "user1",
		"realm":    "myrealm",
		"email":    "user1@example.com",
		"credentials": []any{
			map[string]any{"type": "password", "value": "hunter2", "temporary": false},
		},
	}
	res, err := moduleKeycloakUser(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	// Verify the password value never appears in any rendered command
	// line (this port pipes it over stdin instead — see
	// keycloak_user.go's own doc comment).
	for _, cmd := range conn.Commands {
		if strings.Contains(cmd, "hunter2") {
			t.Fatalf("password leaked into command line: %q", cmd)
		}
	}
}
