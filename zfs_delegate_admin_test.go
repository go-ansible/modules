package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleZfsDelegateAdminGrantUser(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs allow -ld -u adm allow,unallow rpool/myfs": {RC: 0},
	})
	res, err := moduleZfsDelegateAdmin(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "users": []string{"adm"}, "permissions": []string{"allow", "unallow"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleZfsDelegateAdminGroupsAndEveryone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs allow -ld -g backup send rpool/myvol": {RC: 0},
		"zfs allow -ld -e send rpool/myvol":        {RC: 0},
	})
	res, err := moduleZfsDelegateAdmin(context.Background(), conn, map[string]any{
		"name": "rpool/myvol", "groups": []string{"backup"}, "everyone": true, "permissions": []string{"send"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleZfsDelegateAdminLocalScope(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs allow -l -u foo,bar send,receive rpool/myfs": {RC: 0},
	})
	res, err := moduleZfsDelegateAdmin(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "users": []string{"foo", "bar"}, "permissions": []string{"send", "receive"}, "local": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZfsDelegateAdminUnallowEveryone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs unallow -ld -e rpool/myfs": {RC: 0},
	})
	res, err := moduleZfsDelegateAdmin(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "everyone": true, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZfsDelegateAdminUnallowRecursive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs unallow -r -ld -u adm rpool/myfs": {RC: 0},
	})
	res, err := moduleZfsDelegateAdmin(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "users": []string{"adm"}, "state": "absent", "recursive": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZfsDelegateAdminPresentRequiresPermissions(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZfsDelegateAdmin(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "users": []string{"adm"},
	}); err == nil {
		t.Fatal("want error: permissions required for state=present")
	}
}

func TestModuleZfsDelegateAdminPresentRequiresEntity(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZfsDelegateAdmin(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "permissions": []string{"send"},
	}); err == nil {
		t.Fatal("want error: at least one of users/groups/everyone required")
	}
}

func TestModuleZfsDelegateAdminAbsentClearAllUnsupported(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZfsDelegateAdmin(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "state": "absent",
	}); err == nil {
		t.Fatal("want error: clear-all mode is not supported by this port")
	}
}
