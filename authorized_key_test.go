package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const testPubKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAB charlie@example.com"

func TestModuleAuthorizedKeyPresentNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd charlie": {RC: 0, Stdout: "charlie:x:1000:1000:Charlie:/home/charlie:/bin/bash"},
		"mkdir -p /home/charlie/.ssh && chmod 700 /home/charlie/.ssh":                             {RC: 0},
		"grep -qxF " + shellQuote(testPubKey) + " /home/charlie/.ssh/authorized_keys 2>/dev/null": {RC: 1},
		"printf '%s\\n' " + shellQuote(testPubKey) + " >> /home/charlie/.ssh/authorized_keys":     {RC: 0},
	})
	res, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{"user": "charlie", "key": testPubKey})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAuthorizedKeyPresentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd charlie": {RC: 0, Stdout: "charlie:x:1000:1000:Charlie:/home/charlie:/bin/bash"},
		"mkdir -p /home/charlie/.ssh && chmod 700 /home/charlie/.ssh":                             {RC: 0},
		"grep -qxF " + shellQuote(testPubKey) + " /home/charlie/.ssh/authorized_keys 2>/dev/null": {RC: 0},
	})
	res, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{"user": "charlie", "key": testPubKey})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAuthorizedKeyAbsent(t *testing.T) {
	path := "/home/charlie/.ssh/authorized_keys"
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd charlie": {RC: 0, Stdout: "charlie:x:1000:1000:Charlie:/home/charlie:/bin/bash"},
		"mkdir -p /home/charlie/.ssh && chmod 700 /home/charlie/.ssh":                   {RC: 0},
		"test -e " + shellQuote(path):                                                   {RC: 0},
		"grep -qxF " + shellQuote(testPubKey) + " " + shellQuote(path) + " 2>/dev/null": {RC: 0},
		"grep -vxF " + shellQuote(testPubKey) + " " + shellQuote(path) + " > " + shellQuote(path+".tmp") +
			" && mv " + shellQuote(path+".tmp") + " " + shellQuote(path): {RC: 0},
	})
	res, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{
		"user": "charlie", "key": testPubKey, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAuthorizedKeyAbsentNoFile(t *testing.T) {
	path := "/home/charlie/.ssh/authorized_keys"
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd charlie": {RC: 0, Stdout: "charlie:x:1000:1000:Charlie:/home/charlie:/bin/bash"},
		"mkdir -p /home/charlie/.ssh && chmod 700 /home/charlie/.ssh": {RC: 0},
		"test -e " + shellQuote(path):                                 {RC: 1},
	})
	res, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{
		"user": "charlie", "key": testPubKey, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAuthorizedKeyExplicitPath(t *testing.T) {
	path := "/etc/ssh/authorized_keys/charlie"
	conn := newFakeConn(map[string]remoteexec.Result{
		"mkdir -p /etc/ssh/authorized_keys && chmod 700 /etc/ssh/authorized_keys":       {RC: 0},
		"grep -qxF " + shellQuote(testPubKey) + " " + shellQuote(path) + " 2>/dev/null": {RC: 1},
		"printf '%s\\n' " + shellQuote(testPubKey) + " >> " + shellQuote(path):          {RC: 0},
	})
	res, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{
		"user": "charlie", "key": testPubKey, "path": path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	for _, c := range conn.Commands {
		if c == "getent passwd charlie" {
			t.Fatal("explicit path should skip home dir resolution")
		}
	}
}

func TestModuleAuthorizedKeyExclusive(t *testing.T) {
	path := "/home/charlie/.ssh/authorized_keys"
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd charlie": {RC: 0, Stdout: "charlie:x:1000:1000:Charlie:/home/charlie:/bin/bash"},
		"mkdir -p /home/charlie/.ssh && chmod 700 /home/charlie/.ssh": {RC: 0},
		"test -e " + shellQuote(path):                                 {RC: 0},
		"cat " + shellQuote(path):                                     {RC: 0, Stdout: "old-key\n"},
	})
	res, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{
		"user": "charlie", "key": testPubKey, "exclusive": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAuthorizedKeyValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing user")
	}
	if _, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{"user": "u"}); err == nil {
		t.Fatal("want error for missing key")
	}
	if _, err := moduleAuthorizedKey(context.Background(), conn, map[string]any{"user": "u", "key": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}
