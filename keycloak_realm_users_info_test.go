package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakRealmUsersInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get users -r MyCustomRealm": {
			RC: 0, Stdout: `[{"id":"1234-5678-90","username":"user1","email":"user1@example.com"}]`,
		},
	})
	res, err := moduleKeycloakRealmUsersInfo(context.Background(), conn, map[string]any{"realm": "MyCustomRealm"})
	if err != nil {
		t.Fatal(err)
	}
	users := res.Extra["users"].([]map[string]any)
	if len(users) != 1 || users[0]["username"] != "user1" {
		t.Fatalf("users = %#v", users)
	}
}
