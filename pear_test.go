package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePearInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pear list":             {RC: 0, Stdout: ""},
		"pear install Net_URL2": {RC: 0},
	})
	res, err := modulePear(context.Background(), conn, map[string]any{"name": "Net_URL2"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePearPeclPackage(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pecl list":              {RC: 0, Stdout: ""},
		"pecl install json_post": {RC: 0},
	})
	res, err := modulePear(context.Background(), conn, map[string]any{"name": "pecl/json_post"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePearAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pear list": {RC: 0, Stdout: "Net_URL2 1.0.0"},
	})
	res, err := modulePear(context.Background(), conn, map[string]any{"name": "Net_URL2"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePearAbsentMultiplePackages(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pear list":                {RC: 0, Stdout: "Net_URL2 1.0.0"},
		"pecl list":                {RC: 0, Stdout: "json_post 1.0"},
		"pear uninstall Net_URL2":  {RC: 0},
		"pecl uninstall json_post": {RC: 0},
	})
	res, err := modulePear(context.Background(), conn, map[string]any{
		"name": "Net_URL2,pecl/json_post", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 4 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePearCustomExecutable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/opt/pear/bin/pear list":             {RC: 0, Stdout: ""},
		"/opt/pear/bin/pear install Net_URL2": {RC: 0},
	})
	res, err := modulePear(context.Background(), conn, map[string]any{
		"name": "Net_URL2", "executable": "/opt/pear/bin/pear",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePearMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePear(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
