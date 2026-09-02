package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSysvinitStartAlreadyRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/etc/init.d/httpd status >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleSysvinit(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSysvinitStartNotRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/etc/init.d/httpd status >/dev/null 2>&1": {RC: 3},
		"/etc/init.d/httpd start":                  {RC: 0},
	})
	res, err := moduleSysvinit(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysvinitStopWithPattern(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ps -ef 2>/dev/null | grep -F " + shellQuote("myproc") + " | grep -qv grep": {RC: 0},
		"/etc/init.d/httpd stop": {RC: 0},
	})
	res, err := moduleSysvinit(context.Background(), conn, map[string]any{
		"name": "httpd", "state": "stopped", "pattern": "myproc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysvinitRestarted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/etc/init.d/httpd stop":  {RC: 0},
		"sleep 1":                 {RC: 0},
		"/etc/init.d/httpd start": {RC: 0},
	})
	res, err := moduleSysvinit(context.Background(), conn, map[string]any{"name": "httpd", "state": "restarted"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysvinitEnabledChkconfig(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v chkconfig >/dev/null 2>&1": {RC: 0},
		"chkconfig httpd on":                   {RC: 0},
	})
	res, err := moduleSysvinit(context.Background(), conn, map[string]any{"name": "httpd", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysvinitEnabledUpdateRcD(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v chkconfig >/dev/null 2>&1": {RC: 1},
		"update-rc.d httpd enable":             {RC: 0},
	})
	res, err := moduleSysvinit(context.Background(), conn, map[string]any{"name": "httpd", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysvinitInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSysvinit(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleSysvinitMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSysvinit(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
