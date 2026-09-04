package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDjangoLoadDataBasic(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django loaddata --no-color --settings myproject.settings" +
		" --database default --format json fixture1.json fixture2.json"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoLoadData(context.Background(), conn, map[string]any{
		"settings": "myproject.settings",
		"fixtures": []any{"fixture1.json", "fixture2.json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDjangoLoadDataAllFlags(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django loaddata --no-color --settings s" +
		" --database mydb --ignorenonexistent --app myapp --format yaml --exclude auth f.yaml"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoLoadData(context.Background(), conn, map[string]any{
		"settings":            "s",
		"database":            "mydb",
		"ignore_non_existent": true,
		"app":                 "myapp",
		"format":              "yaml",
		"excludes":            []any{"auth"},
		"fixtures":            []any{"f.yaml"},
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

func TestModuleDjangoLoadDataMissingSettings(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoLoadData(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing settings")
	}
}

func TestModuleDjangoLoadDataNonZero(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django loaddata --no-color --settings s --database default --format json"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 1, Stderr: "no fixture named foo"},
	})
	res, err := moduleDjangoLoadData(context.Background(), conn, map[string]any{"settings": "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-zero exit")
	}
}
