package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDjangoCommandBasic(t *testing.T) {
	want := "env LC_ALL=C python -m django check --no-color"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoCommand(context.Background(), conn, map[string]any{"command": "check"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != want {
		t.Fatalf("commands = %v, want [%q]", conn.Commands, want)
	}
}

func TestModuleDjangoCommandVenv(t *testing.T) {
	want := "env LC_ALL=C /opt/venv/bin/python -m django migrate --no-color"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoCommand(context.Background(), conn, map[string]any{
		"command": "migrate", "venv": "/opt/venv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoCommandAllFlags(t *testing.T) {
	want := "env LC_ALL=C python -m django migrate --no-color --pythonpath /x --settings proj.settings" +
		" --skip-checks --traceback --verbosity 2 --noinput"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoCommand(context.Background(), conn, map[string]any{
		"command":     "migrate",
		"pythonpath":  "/x",
		"settings":    "proj.settings",
		"skip_checks": true,
		"traceback":   true,
		"verbosity":   2,
		"extra_args":  []any{"--noinput"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoCommandNonZero(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"env LC_ALL=C python -m django check --no-color": {RC: 1, Stderr: "boom"},
	})
	res, err := moduleDjangoCommand(context.Background(), conn, map[string]any{"command": "check"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-zero exit")
	}
	if res.Extra["stderr"] != "boom" {
		t.Fatalf("stderr = %v", res.Extra["stderr"])
	}
}

func TestModuleDjangoCommandMissingCommand(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoCommand(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing command")
	}
}

func TestModuleDjangoCommandBadVerbosity(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoCommand(context.Background(), conn, map[string]any{
		"command": "check", "verbosity": 5,
	}); err == nil {
		t.Fatal("want error for out-of-range verbosity")
	}
}
