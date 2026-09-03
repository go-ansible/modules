package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSysrcPresentAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sysrc -f /etc/rc.conf -n mysql_pidfile": {RC: 0, Stdout: "/var/run/mysqld/mysqld.pid\n"},
	})
	res, err := moduleSysrc(context.Background(), conn, map[string]any{
		"name": "mysql_pidfile", "value": "/var/run/mysqld/mysqld.pid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSysrcPresentSetsNewValue(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sysrc -f /etc/rc.conf -n nginx_enable":  {RC: 1},
		"sysrc -f /etc/rc.conf nginx_enable=YES": {RC: 0},
	})
	res, err := moduleSysrc(context.Background(), conn, map[string]any{"name": "nginx_enable", "value": "YES"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysrcAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sysrc -f /etc/rc.conf -n foo": {RC: 0, Stdout: "bar\n"},
		"sysrc -f /etc/rc.conf -x foo": {RC: 0},
	})
	res, err := moduleSysrc(context.Background(), conn, map[string]any{"name": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysrcValuePresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sysrc -f /etc/rc.conf -n cloned_interfaces":    {RC: 0, Stdout: "\n"},
		"sysrc -f /etc/rc.conf cloned_interfaces+=gif0": {RC: 0},
	})
	// second query after the +=, returns the updated value
	conn.on["sysrc -f /etc/rc.conf -n cloned_interfaces"] = remoteexec.Result{RC: 0, Stdout: "\n"}
	res, err := moduleSysrc(context.Background(), conn, map[string]any{
		"name": "cloned_interfaces", "state": "value_present", "value": "gif0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	// before == after in this fake (both empty), so Changed can't be trusted;
	// verify the command sequence at least ran the +=.
	found := false
	for _, c := range conn.Commands {
		if c == "sysrc -f /etc/rc.conf cloned_interfaces+=gif0" {
			found = true
		}
	}
	if !found {
		t.Fatal("want value_present to run the += command")
	}
}

func TestModuleSysrcJail(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sysrc -f /etc/rc.conf -j testjail -n nginx_enable":  {RC: 1},
		"sysrc -f /etc/rc.conf -j testjail nginx_enable=YES": {RC: 0},
	})
	res, err := moduleSysrc(context.Background(), conn, map[string]any{
		"name": "nginx_enable", "value": "YES", "jail": "testjail",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysrcUnknownState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSysrc(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleSysrcMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSysrc(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
