package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleYumVersionlockPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yum versionlock list":         {RC: 0, Stdout: ""},
		"yum -q versionlock add httpd": {RC: 0},
	})
	res, err := moduleYumVersionlock(context.Background(), conn, map[string]any{"name": "httpd"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleYumVersionlockAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yum versionlock list": {RC: 0, Stdout: "0:httpd-2.4.57-2.el9.*\n"},
	})
	res, err := moduleYumVersionlock(context.Background(), conn, map[string]any{"name": "httpd"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no add call", conn.Commands)
	}
}

func TestModuleYumVersionlockMultiplePackagesOneCall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yum versionlock list":               {RC: 0, Stdout: ""},
		"yum -q versionlock add httpd nginx": {RC: 0},
	})
	res, err := moduleYumVersionlock(context.Background(), conn, map[string]any{"name": []any{"httpd", "nginx"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYumVersionlockAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yum versionlock list":            {RC: 0, Stdout: "0:httpd-2.4.57-2.el9.*\n"},
		"yum -q versionlock delete httpd": {RC: 0},
	})
	res, err := moduleYumVersionlock(context.Background(), conn, map[string]any{"name": "httpd", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYumVersionlockAbsentNotPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"yum versionlock list": {RC: 0, Stdout: ""},
	})
	res, err := moduleYumVersionlock(context.Background(), conn, map[string]any{"name": "httpd", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleYumVersionlockMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleYumVersionlock(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleYumVersionlockInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleYumVersionlock(context.Background(), conn, map[string]any{"name": "httpd", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}
