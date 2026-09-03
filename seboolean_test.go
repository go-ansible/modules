package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSebooleanOn(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getsebool httpd_can_network_connect":       {RC: 0, Stdout: "httpd_can_network_connect --> off"},
		"setsebool -P httpd_can_network_connect on": {RC: 0},
	})
	res, err := moduleSeboolean(context.Background(), conn, map[string]any{
		"name": "httpd_can_network_connect", "state": true, "persistent": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSebooleanAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getsebool httpd_can_network_connect": {RC: 0, Stdout: "httpd_can_network_connect --> on"},
	})
	res, err := moduleSeboolean(context.Background(), conn, map[string]any{
		"name": "httpd_can_network_connect", "state": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSebooleanOff(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getsebool httpd_can_network_connect":     {RC: 0, Stdout: "httpd_can_network_connect --> on"},
		"setsebool httpd_can_network_connect off": {RC: 0},
	})
	res, err := moduleSeboolean(context.Background(), conn, map[string]any{
		"name": "httpd_can_network_connect", "state": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSebooleanValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSeboolean(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleSeboolean(context.Background(), conn, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error for missing state")
	}
}

func TestModuleSebooleanBadGetsebool(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getsebool x": {RC: 0, Stdout: "garbage"},
	})
	if _, err := moduleSeboolean(context.Background(), conn, map[string]any{"name": "x", "state": true}); err == nil {
		t.Fatal("want error for unparseable getsebool output")
	}
}
