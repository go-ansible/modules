package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDjangoCheckBasic(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django check --no-color --settings proj.settings"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoCheck(context.Background(), conn, map[string]any{"settings": "proj.settings"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["version"] != "5.1.2" {
		t.Fatalf("version = %v", res.Extra["version"])
	}
	if len(conn.Commands) != 2 || conn.Commands[0] != verCmd || conn.Commands[1] != want {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleDjangoCheckAllFlags(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django check --no-color --settings proj.settings" +
		" --database db1 --database db2 --deploy --fail-level WARNING --tag t1 --tag t2 app1 app2"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoCheck(context.Background(), conn, map[string]any{
		"settings":   "proj.settings",
		"databases":  []any{"db1", "db2"},
		"deploy":     true,
		"fail_level": "WARNING",
		"tags":       []any{"t1", "t2"},
		"apps":       []any{"app1", "app2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[1] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[1], want)
	}
}

func TestModuleDjangoCheckMissingSettings(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoCheck(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing settings")
	}
}

func TestModuleDjangoCheckBadFailLevel(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoCheck(context.Background(), conn, map[string]any{
		"settings": "proj.settings", "fail_level": "BOGUS",
	}); err == nil {
		t.Fatal("want error for invalid fail_level")
	}
}

func TestModuleDjangoCheckVersionProbeFails(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 1, Stderr: "No module named django"},
	})
	res, err := moduleDjangoCheck(context.Background(), conn, map[string]any{"settings": "proj.settings"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the version probe fails")
	}
}

func TestModuleDjangoCheckNonZero(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django check --no-color --settings proj.settings"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 1, Stderr: "SystemCheckError"},
	})
	res, err := moduleDjangoCheck(context.Background(), conn, map[string]any{"settings": "proj.settings"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-zero exit")
	}
}
