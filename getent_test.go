package modules

import (
	"context"
	"reflect"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestGetentCmd(t *testing.T) {
	if got := getentCmd("passwd", ""); got != "getent passwd" {
		t.Fatalf("cmd = %q", got)
	}
	if got := getentCmd("passwd", "root"); got != "getent passwd root" {
		t.Fatalf("cmd = %q", got)
	}
}

func TestParseGetentColon(t *testing.T) {
	out := "root:x:0:0:root:/root:/bin/bash\n"
	entries := parseGetent(out, ":")
	want := map[string]any{"root": []string{"x", "0", "0", "root", "/root", "/bin/bash"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
}

func TestParseGetentHosts(t *testing.T) {
	out := "127.0.0.1\tlocalhost localhost.localdomain\n"
	entries := parseGetent(out, " ")
	if _, ok := entries["127.0.0.1"]; !ok {
		t.Fatalf("entries = %v", entries)
	}
}

func TestParseGetentEmptyLinesSkipped(t *testing.T) {
	out := "\n\nroot:x:0:0:root:/root:/bin/bash\n\n"
	entries := parseGetent(out, ":")
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}
}

func TestModuleGetentAllEntries(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd": {RC: 0, Stdout: "root:x:0:0:root:/root:/bin/bash\nbob:x:1000:1000::/home/bob:/bin/sh\n"},
	})
	res, err := moduleGetent(context.Background(), conn, map[string]any{"database": "passwd"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	entries := res.Extra["getent_passwd"].(map[string]any)
	if len(entries) != 2 {
		t.Fatalf("entries = %v", entries)
	}
}

func TestModuleGetentWithKey(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd root": {RC: 0, Stdout: "root:x:0:0:root:/root:/bin/bash\n"},
	})
	res, err := moduleGetent(context.Background(), conn, map[string]any{"database": "passwd", "key": "root"})
	if err != nil {
		t.Fatal(err)
	}
	entries := res.Extra["getent_passwd"].(map[string]any)
	if _, ok := entries["root"]; !ok {
		t.Fatalf("entries = %v", entries)
	}
}

func TestModuleGetentKeyNotFoundFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd ghost": {RC: 2},
	})
	res, err := moduleGetent(context.Background(), conn, map[string]any{"database": "passwd", "key": "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when key is not found and fail_key is true")
	}
}

func TestModuleGetentKeyNotFoundNoFail(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd ghost": {RC: 2},
	})
	res, err := moduleGetent(context.Background(), conn, map[string]any{
		"database": "passwd", "key": "ghost", "fail_key": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want not Failed when fail_key is false")
	}
	entries := res.Extra["getent_passwd"].(map[string]any)
	if len(entries) != 0 {
		t.Fatalf("entries = %v", entries)
	}
}

func TestModuleGetentHostsSplit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent hosts": {RC: 0, Stdout: "127.0.0.1 localhost localhost.localdomain\n"},
	})
	res, err := moduleGetent(context.Background(), conn, map[string]any{"database": "hosts"})
	if err != nil {
		t.Fatal(err)
	}
	entries := res.Extra["getent_hosts"].(map[string]any)
	got, ok := entries["127.0.0.1"].([]string)
	if !ok || len(got) != 2 || got[0] != "localhost" {
		t.Fatalf("entries = %v", entries)
	}
}

func TestModuleGetentCustomSplit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent group": {RC: 0, Stdout: "wheel;x;10;bob,alice\n"},
	})
	res, err := moduleGetent(context.Background(), conn, map[string]any{"database": "group", "split": ";"})
	if err != nil {
		t.Fatal(err)
	}
	entries := res.Extra["getent_group"].(map[string]any)
	got, ok := entries["wheel"].([]string)
	if !ok || len(got) != 3 || got[2] != "bob,alice" {
		t.Fatalf("entries = %v", entries)
	}
}

func TestModuleGetentMissingDatabase(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGetent(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing database")
	}
}
