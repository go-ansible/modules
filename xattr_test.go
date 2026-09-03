package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXattrReadMissingKey(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXattr(context.Background(), conn, map[string]any{"path": "/etc/foo.conf"}); err == nil {
		t.Fatal("want error: key required for state=read")
	}
}

func TestModuleXattrRead(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr --only-values -n user.foo /etc/foo.conf 2>/dev/null": {RC: 0, Stdout: "bar"},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "key": "foo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("read must not change anything")
	}
	got := res.Extra["xattr"].(map[string]any)
	if got["user.foo"] != "bar" {
		t.Fatalf("xattr = %v", got)
	}
}

func TestModuleXattrSetNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr --only-values -n user.foo /etc/foo.conf 2>/dev/null": {RC: 1},
		"setfattr -n user.foo -v bar /etc/foo.conf":                    {RC: 0},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "key": "foo", "value": "bar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleXattrSetAlreadyCorrect(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr --only-values -n user.foo /etc/foo.conf 2>/dev/null": {RC: 0, Stdout: "bar"},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "key": "foo", "value": "bar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleXattrNamespace(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr --only-values -n trusted.glusterfs.volume-id /mnt/bricks/brick1 2>/dev/null":             {RC: 1},
		"setfattr -n trusted.glusterfs.volume-id -v 0x817b94343f164f199e5b573b4ea1f914 /mnt/bricks/brick1": {RC: 0},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/mnt/bricks/brick1", "namespace": "trusted", "key": "glusterfs.volume-id",
		"value": "0x817b94343f164f199e5b573b4ea1f914",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleXattrAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr --only-values -n user.foo /etc/foo.conf 2>/dev/null": {RC: 0, Stdout: "bar"},
		"setfattr -x user.foo /etc/foo.conf":                           {RC: 0},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "key": "foo", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleXattrAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr --only-values -n user.foo /etc/foo.conf 2>/dev/null": {RC: 1},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "key": "foo", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleXattrFollowFalseUsesHFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr -h --only-values -n user.foo /etc/foo.conf 2>/dev/null": {RC: 0, Stdout: "bar"},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "key": "foo", "follow": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleXattrAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr -d /etc/foo.conf 2>/dev/null": {RC: 0, Stdout: "# file: etc/foo.conf\nuser.foo=\"bar\"\nuser.baz=\"qux\"\n"},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "state": "all",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Extra["xattr"].(map[string]any)
	if got["user.foo"] != "bar" || got["user.baz"] != "qux" {
		t.Fatalf("xattr = %v", got)
	}
}

func TestModuleXattrKeys(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfattr -d /etc/foo.conf 2>/dev/null": {RC: 0, Stdout: "# file: etc/foo.conf\nuser.foo=\"bar\"\n"},
	})
	res, err := moduleXattr(context.Background(), conn, map[string]any{
		"path": "/etc/foo.conf", "state": "keys",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Extra["xattr"].([]string)
	if len(got) != 1 || got[0] != "user.foo" {
		t.Fatalf("keys = %v", got)
	}
}

func TestModuleXattrMissingPath(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXattr(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
}
