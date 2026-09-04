package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSimpleinitMsbMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSimpleinitMsb(context.Background(), conn, map[string]any{"name": "httpd"}); err == nil {
		t.Fatal("want error when neither state nor enabled is given")
	}
}

func TestModuleSimpleinitMsbTelinitNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v telinit":        {RC: 1},
		"test -e /sbin/telinit":     {RC: 1},
		"test -e /usr/sbin/telinit": {RC: 1},
		"test -e /bin/telinit":      {RC: 1},
		"test -e /usr/bin/telinit":  {RC: 1},
	})
	res, err := moduleSimpleinitMsb(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when telinit cannot be found")
	}
}

func TestModuleSimpleinitMsbAlreadyStarted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v telinit":             {RC: 0, Stdout: "/sbin/telinit\n"},
		"test -e /etc/init.d/smgl_init":  {RC: 0},
		"/sbin/telinit list":             {RC: 0, Stdout: "running httpd\n"},
		"/sbin/telinit run httpd status": {RC: 0, Stdout: "httpd is running\n"},
	})
	res, err := moduleSimpleinitMsb(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Fatal("want unchanged: already running")
	}
	if res.Extra["state"] != "started" {
		t.Fatalf("state = %v", res.Extra["state"])
	}
}

func TestModuleSimpleinitMsbStartsStoppedService(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v telinit":             {RC: 0, Stdout: "/sbin/telinit\n"},
		"test -e /etc/init.d/smgl_init":  {RC: 0},
		"/sbin/telinit list":             {RC: 0, Stdout: "stopped httpd\n"},
		"/sbin/telinit run httpd status": {RC: 0, Stdout: "httpd is not running\n"},
		"/sbin/telinit run httpd start":  {RC: 0},
	})
	res, err := moduleSimpleinitMsb(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSimpleinitMsbUnknownService(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v telinit":            {RC: 0, Stdout: "/sbin/telinit\n"},
		"test -e /etc/init.d/smgl_init": {RC: 0},
		"/sbin/telinit list":            {RC: 0, Stdout: "running sshd\n"},
	})
	res, err := moduleSimpleinitMsb(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a service telinit list doesn't know about")
	}
}

func TestModuleSimpleinitMsbEnable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v telinit":             {RC: 0, Stdout: "/sbin/telinit\n"},
		"test -e /etc/init.d/smgl_init":  {RC: 0},
		"/sbin/telinit list":             {RC: 0, Stdout: "running httpd\n"},
		"/sbin/telinit Trued":            {RC: 0, Stdout: ""},
		"/sbin/telinit bootenable httpd": {RC: 0},
	})
	res, err := moduleSimpleinitMsb(context.Background(), conn, map[string]any{"name": "httpd", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["enabled"] != true {
		t.Fatalf("enabled = %v", res.Extra["enabled"])
	}
}
