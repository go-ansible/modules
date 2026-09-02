package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// These modules mutate real OS state (users, groups, packages,
// services) and typically need root, so they're tested against a
// scripted fakeConn (see fakeconn_test.go) rather than the real OS —
// verifying the module issues the right commands and reacts correctly
// to their scripted results, not that a real useradd/apt-get run.

func TestModuleUserCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd bob": {RC: 2}, // not found
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[1] != "useradd -m bob" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleUserAlreadyExistsNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd bob": {RC: 0},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: exists, nothing else requested")
	}
}

func TestModuleUserAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd bob": {RC: 0},
		"userdel -r bob":    {RC: 0},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleUserAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd bob": {RC: 2},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleUserMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUser(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleGroupCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent group devs": {RC: 2},
	})
	res, err := moduleGroup(context.Background(), conn, map[string]any{"name": "devs"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[1] != "groupadd devs" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleGroupAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent group devs": {RC: 0},
	})
	res, err := moduleGroup(context.Background(), conn, map[string]any{"name": "devs"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleGroupAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent group devs": {RC: 0},
		"groupdel devs":     {RC: 0},
	})
	res, err := moduleGroup(context.Background(), conn, map[string]any{"name": "devs", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSystemdStart(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl is-active nginx": {RC: 3}, // inactive
		"systemctl start nginx":     {RC: 0},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"name": "nginx", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSystemdAlreadyStarted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl is-active nginx": {RC: 0}, // already active
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"name": "nginx", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSystemdEnable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl is-enabled nginx": {RC: 1}, // disabled
		"systemctl enable nginx":     {RC: 0},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"name": "nginx", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSystemdUnknownState(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl is-active nginx": {RC: 0},
	})
	if _, err := moduleSystemd(context.Background(), conn, map[string]any{"name": "nginx", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleAptInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg -s curl 2>/dev/null | grep -q '^Status:.*installed'":  {RC: 1},
		"DEBIAN_FRONTEND=noninteractive apt-get install -y -q curl": {RC: 0},
	})
	res, err := moduleApt(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg -s curl 2>/dev/null | grep -q '^Status:.*installed'": {RC: 0},
	})
	res, err := moduleApt(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAptAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg -s curl 2>/dev/null | grep -q '^Status:.*installed'": {RC: 0},
		"DEBIAN_FRONTEND=noninteractive apt-get remove -y -q curl": {RC: 0},
	})
	res, err := moduleApt(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleApt(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModulePipInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip3 show requests >/dev/null 2>&1": {RC: 1},
		"pip3 install requests":              {RC: 0},
	})
	res, err := modulePip(context.Background(), conn, map[string]any{"name": "requests"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePipAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip3 show requests >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePip(context.Background(), conn, map[string]any{"name": "requests"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestPkgName(t *testing.T) {
	cases := map[string]string{
		"requests==2.31.0": "requests",
		"requests>=2.0":    "requests",
		"requests":         "requests",
	}
	for in, want := range cases {
		if got := pkgName(in); got != want {
			t.Errorf("pkgName(%q) = %q, want %q", in, got, want)
		}
	}
}
