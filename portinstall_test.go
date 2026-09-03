package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePortinstallInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg info -e foo >/dev/null 2>&1":        {RC: 1},
		"portinstall --batch --use-packages foo": {RC: 0},
	})
	res, err := modulePortinstall(context.Background(), conn, map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePortinstallInstallFailurePropagates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg info -e foo >/dev/null 2>&1":        {RC: 1},
		"portinstall --batch --use-packages foo": {RC: 1, Stderr: "no matches for package name foo"},
	})
	if _, err := modulePortinstall(context.Background(), conn, map[string]any{"name": "foo"}); err == nil {
		t.Fatal("want error when portinstall exits non-zero")
	}
}

func TestModulePortinstallNoUsePackages(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg info -e foo >/dev/null 2>&1": {RC: 1},
		"portinstall --batch foo":         {RC: 0},
	})
	res, err := modulePortinstall(context.Background(), conn, map[string]any{"name": "foo", "use_packages": false})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePortinstallAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg info -e foo >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePortinstall(context.Background(), conn, map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePortinstallCommaSeparatedNames(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg info -e foo >/dev/null 2>&1": {RC: 0},
		"pkg info -e bar >/dev/null 2>&1": {RC: 0},
		"pkg delete -y foo":               {RC: 0},
		"pkg delete -y bar":               {RC: 0},
	})
	res, err := modulePortinstall(context.Background(), conn, map[string]any{"name": "foo,bar", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if len(conn.Commands) != 4 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePortinstallAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg info -e foo >/dev/null 2>&1": {RC: 1},
	})
	res, err := modulePortinstall(context.Background(), conn, map[string]any{"name": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePortinstallMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePortinstall(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModulePortinstallInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePortinstall(context.Background(), conn, map[string]any{"name": "foo", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
