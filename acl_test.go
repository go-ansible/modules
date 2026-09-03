package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAclQuery(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfacl /etc/foo.conf": {RC: 0, Stdout: "# file: foo.conf\n# owner: root\nuser::rwx\ngroup::r-x\nother::r--\n"},
	})
	res, err := moduleAcl(context.Background(), conn, map[string]any{"path": "/etc/foo.conf"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged for query")
	}
	acl, ok := res.Extra["acl"].([]string)
	if !ok || len(acl) != 3 {
		t.Fatalf("acl = %v", res.Extra["acl"])
	}
}

func TestModuleAclPresentNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfacl /etc/foo.conf":               {RC: 0, Stdout: "user::rwx\n"},
		"setfacl -m user:joe:r /etc/foo.conf": {RC: 0},
	})
	res, err := moduleAcl(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "state": "present", "etype": "user", "entity": "joe", "permissions": "r",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAclPresentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfacl /etc/foo.conf": {RC: 0, Stdout: "user:joe:r\n"},
	})
	res, err := moduleAcl(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "state": "present", "etype": "user", "entity": "joe", "permissions": "r",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAclAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfacl /etc/foo.conf":             {RC: 0, Stdout: "user:joe:r\n"},
		"setfacl -x user:joe /etc/foo.conf": {RC: 0},
	})
	res, err := moduleAcl(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "state": "absent", "etype": "user", "entity": "joe",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAclAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfacl /etc/foo.conf": {RC: 0, Stdout: "user::rwx\n"},
	})
	res, err := moduleAcl(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "state": "absent", "etype": "user", "entity": "joe",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAclDefaultRecursive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfacl /etc/foo.d":                     {RC: 0, Stdout: ""},
		"setfacl -R -m d:user:joe:rw /etc/foo.d": {RC: 0},
	})
	res, err := moduleAcl(context.Background(), conn, map[string]any{
		"path": "/etc/foo.d", "state": "present", "etype": "user", "entity": "joe",
		"permissions": "rw", "default": true, "recursive": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAclEntryRaw(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"setfacl -m u:bob:rwx /etc/foo.conf": {RC: 0},
		"getfacl /etc/foo.conf":              {RC: 0, Stdout: "user:bob:rwx\n"},
	})
	res, err := moduleAcl(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "state": "present", "entry": "u:bob:rwx",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed (entry form always applies)")
	}
}

func TestModuleAclValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAcl(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
	if _, err := moduleAcl(context.Background(), conn, map[string]any{"path": "/x", "state": "present"}); err == nil {
		t.Fatal("want error for missing etype/entry")
	}
	if _, err := moduleAcl(context.Background(), conn, map[string]any{"path": "/x", "state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}
