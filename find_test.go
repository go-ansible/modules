package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestFindCmd(t *testing.T) {
	cmd, err := findCmd([]string{"/a"}, nil, false, "file")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "find /a -mindepth 1 -maxdepth 1 -type f" {
		t.Fatalf("cmd = %q", cmd)
	}

	cmd, err = findCmd([]string{"/a", "/b"}, []string{"*.txt", "*.log"}, true, "directory")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != `find /a /b -mindepth 1 -type d \( -name '*.txt' -o -name '*.log' \)` {
		t.Fatalf("cmd = %q", cmd)
	}

	cmd, err = findCmd([]string{"/a"}, nil, false, "any")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "find /a -mindepth 1 -maxdepth 1" {
		t.Fatalf("cmd = %q", cmd)
	}

	if _, err := findCmd([]string{"/a"}, nil, false, "bogus"); err == nil {
		t.Fatal("want error for invalid file_type")
	}
}

func TestModuleFindResults(t *testing.T) {
	cmd, err := findCmd([]string{"/a"}, nil, false, "file")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "/a/one.txt\n/a/two.txt\n"},
	})
	res, err := moduleFind(context.Background(), conn, map[string]any{"paths": "/a"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	files := res.Extra["files"].([]map[string]any)
	if len(files) != 2 {
		t.Fatalf("files = %v", files)
	}
	if files[0]["path"] != "/a/one.txt" || files[1]["path"] != "/a/two.txt" {
		t.Fatalf("files = %v", files)
	}
	if res.Extra["matched"] != 2 {
		t.Fatalf("matched = %v", res.Extra["matched"])
	}
}

func TestModuleFindEmpty(t *testing.T) {
	cmd, err := findCmd([]string{"/empty"}, nil, false, "file")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: ""},
	})
	res, err := moduleFind(context.Background(), conn, map[string]any{"paths": []string{"/empty"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["matched"] != 0 {
		t.Fatalf("matched = %v", res.Extra["matched"])
	}
}

func TestModuleFindToleratesNonZeroExit(t *testing.T) {
	// A permission-denied subdirectory makes real find exit non-zero
	// while still printing everything else it found; moduleFind should
	// not treat that as a hard failure.
	cmd, err := findCmd([]string{"/a"}, nil, true, "file")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 1, Stdout: "/a/ok.txt\n", Stderr: "find: /a/locked: Permission denied"},
	})
	res, err := moduleFind(context.Background(), conn, map[string]any{"paths": "/a", "recurse": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want not Failed despite find's non-zero exit")
	}
	if res.Extra["matched"] != 1 {
		t.Fatalf("matched = %v", res.Extra["matched"])
	}
}

func TestModuleFindMissingPaths(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFind(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing paths")
	}
}
