package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMacportsInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -q installed foo": {RC: 0, Stdout: ""},
		"port install foo":      {RC: 0},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleMacportsInstallWithVariant(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -q installed foo":           {RC: 0, Stdout: ""},
		"port install foo +universal+x11": {RC: 0},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo", "variant": "+universal+x11"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMacportsAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -q installed foo": {RC: 0, Stdout: "foo @1.0_0 (active)\n"},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMacportsAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -q installed foo": {RC: 0, Stdout: "foo @1.0_0 (active)\n"},
		"port uninstall foo":    {RC: 0},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMacportsActivate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -q installed foo": {RC: 0, Stdout: "foo @1.0_0\n"},
		"port activate foo":     {RC: 0},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo", "state": "active"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMacportsActivateAlreadyActive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -q installed foo": {RC: 0, Stdout: "foo @1.0_0 (active)\n"},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo", "state": "active"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMacportsActivateNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -q installed foo": {RC: 0, Stdout: ""},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo", "state": "active"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModuleMacportsDeactivate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -q installed foo": {RC: 0, Stdout: "foo @1.0_0 (active)\n"},
		"port deactivate foo":   {RC: 0},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo", "state": "inactive"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMacportsSelfupdateChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -v selfupdate": {RC: 0, Stdout: "MacPorts base version 2.8.1 installed\nTotal number of ports parsed: 5\n"},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"selfupdate": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMacportsSelfupdateUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port -v selfupdate": {RC: 0, Stdout: "MacPorts base version 2.8.1 installed\nTotal number of ports parsed: 0\n"},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"selfupdate": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMacportsUpgradeNothingToDo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port upgrade outdated": {RC: 1, Stdout: "Nothing to upgrade."},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"upgrade": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMacportsUpgradeChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"port upgrade outdated": {RC: 0, Stdout: "--->  Upgrading foo\n"},
	})
	res, err := moduleMacports(context.Background(), conn, map[string]any{"upgrade": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMacportsNothingRequired(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMacports(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: at least one of name/selfupdate/upgrade is required")
	}
}

func TestModuleMacportsInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMacports(context.Background(), conn, map[string]any{"name": "foo", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
