package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDjangoCreateCacheTableBasic(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django createcachetable --no-color --settings proj.settings --database default"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoCreateCacheTable(context.Background(), conn, map[string]any{"settings": "proj.settings"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["version"] != "5.1.2" {
		t.Fatalf("version = %v", res.Extra["version"])
	}
}

func TestModuleDjangoCreateCacheTableDatabase(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django createcachetable --no-color --settings fancysite.settings" +
		" --pythonpath /home/joedoe/project/fancysite --database myotherdb"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoCreateCacheTable(context.Background(), conn, map[string]any{
		"settings":   "fancysite.settings",
		"database":   "myotherdb",
		"pythonpath": "/home/joedoe/project/fancysite",
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

func TestModuleDjangoCreateCacheTableMissingSettings(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoCreateCacheTable(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing settings")
	}
}
