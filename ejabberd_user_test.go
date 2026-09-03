package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleEjabberdUserCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ejabberdctl check_account test server":     {RC: 1},
		"ejabberdctl register test server password": {RC: 0},
	})
	res, err := moduleEjabberdUser(context.Background(), conn, map[string]any{
		"username": "test", "host": "server", "password": "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleEjabberdUserAlreadyPresentSamePassword(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ejabberdctl check_account test server":           {RC: 0},
		"ejabberdctl check_password test server password": {RC: 0},
	})
	res, err := moduleEjabberdUser(context.Background(), conn, map[string]any{
		"username": "test", "host": "server", "password": "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: password already matches")
	}
}

func TestModuleEjabberdUserChangesPassword(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ejabberdctl check_account test server":           {RC: 0},
		"ejabberdctl check_password test server newpass":  {RC: 1},
		"ejabberdctl change_password test server newpass": {RC: 0},
	})
	res, err := moduleEjabberdUser(context.Background(), conn, map[string]any{
		"username": "test", "host": "server", "password": "newpass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: password differs")
	}
}

func TestModuleEjabberdUserDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ejabberdctl check_account test server": {RC: 0},
		"ejabberdctl unregister test server":    {RC: 0},
	})
	res, err := moduleEjabberdUser(context.Background(), conn, map[string]any{
		"username": "test", "host": "server", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleEjabberdUserDeleteAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ejabberdctl check_account test server": {RC: 1},
	})
	res, err := moduleEjabberdUser(context.Background(), conn, map[string]any{
		"username": "test", "host": "server", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already absent")
	}
}

func TestModuleEjabberdUserPresentRequiresPassword(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleEjabberdUser(context.Background(), conn, map[string]any{
		"username": "test", "host": "server",
	})
	if err == nil {
		t.Fatal("want error: password required for state=present")
	}
}
