package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDjangoDumpDataBasic(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django dumpdata --no-color --settings myproject.settings" +
		" --format json --database default --output /tmp/mydata.json"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoDumpData(context.Background(), conn, map[string]any{
		"settings": "myproject.settings",
		"fixture":  "/tmp/mydata.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDjangoDumpDataOutputAlias(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django dumpdata --no-color --settings myproject.settings" +
		" --format json --exclude auth --exclude contenttypes --database myotherdb --output /tmp/mydata.json.gz"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoDumpData(context.Background(), conn, map[string]any{
		"settings": "myproject.settings",
		"database": "myotherdb",
		"excludes": []any{"auth", "contenttypes"},
		"output":   "/tmp/mydata.json.gz",
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

func TestModuleDjangoDumpDataAllFlags(t *testing.T) {
	verCmd := "env LC_ALL=C python -m django --version"
	want := "env LC_ALL=C python -m django dumpdata --no-color --settings s" +
		" --all --format yaml --indent 2 --database default --natural-foreign --natural-primary" +
		" --pks 1,2,3 --output /tmp/f.yaml app.Model"
	conn := newFakeConn(map[string]remoteexec.Result{
		verCmd: {RC: 0, Stdout: "5.1.2\n"},
		want:   {RC: 0},
	})
	res, err := moduleDjangoDumpData(context.Background(), conn, map[string]any{
		"settings":        "s",
		"fixture":         "/tmp/f.yaml",
		"all":             true,
		"format":          "yaml",
		"indent":          2,
		"natural_foreign": true,
		"natural_primary": true,
		"primary_keys":    []any{"1", "2", "3"},
		"apps_models":     []any{"app.Model"},
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

func TestModuleDjangoDumpDataMissingFixture(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoDumpData(context.Background(), conn, map[string]any{"settings": "s"}); err == nil {
		t.Fatal("want error for missing fixture")
	}
}

func TestModuleDjangoDumpDataBadFormat(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoDumpData(context.Background(), conn, map[string]any{
		"settings": "s", "fixture": "/tmp/f", "format": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid format")
	}
}
