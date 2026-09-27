package modules

import (
	"context"
	"os"
	"path/filepath"
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
	// The connection here answers only the find itself, so the stat
	// probe tells us nothing -- and path must survive that, because it
	// comes from the walk rather than from the stat.
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

// Measured from real: examined counts every entry under the roots and
// EXCLUDES the roots themselves, directories included; a path that is
// not a directory is skipped with real's own reason and warning.
func TestFindExaminedAndSkippedPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.txt", "b.log", "sub/c.txt", "sub/deep/d.txt"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	conn := local()

	// Flat: a.txt, b.log, sub -> examined 3, one *.txt matched.
	res, err := moduleFind(ctx, conn, map[string]any{"paths": root, "patterns": "*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["examined"] != 3 || res.Extra["matched"] != 1 {
		t.Errorf("flat: examined=%v matched=%v, want 3 and 1", res.Extra["examined"], res.Extra["matched"])
	}
	// Recursive adds sub/c.txt, sub/deep, sub/deep/d.txt.
	res, err = moduleFind(ctx, conn, map[string]any{"paths": root, "patterns": "*.txt", "recurse": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["examined"] != 6 || res.Extra["matched"] != 3 {
		t.Errorf("recursive: examined=%v matched=%v, want 6 and 3", res.Extra["examined"], res.Extra["matched"])
	}

	// Each matched file carries real's per-file dictionary.
	files, _ := res.Extra["files"].([]map[string]any)
	if len(files) == 0 {
		t.Fatal("no files reported")
	}
	if len(files[0]) != len(findFileKeys) {
		t.Errorf("per-file dict has %d keys, want %d", len(files[0]), len(findFileKeys))
	}
	if files[0]["mode"] != "0644" || files[0]["isreg"] != true {
		t.Errorf("per-file dict: mode=%v isreg=%v", files[0]["mode"], files[0]["isreg"])
	}

	// A path that is not a directory: skipped, with real's wording, and
	// a warning per skipped path whose text ends in a newline.
	notDir := filepath.Join(root, "a.txt")
	res, err = moduleFind(ctx, conn, map[string]any{"paths": []any{root, notDir}, "patterns": "*.txt"})
	if err != nil {
		t.Fatal(err)
	}
	skipped, _ := res.Extra["skipped_paths"].(map[string]any)
	if got := skipped[notDir]; got != "'"+notDir+"' is not a directory" {
		t.Errorf("skipped_paths[%q] = %#v", notDir, got)
	}
	warnings, _ := res.Extra["warnings"].([]any)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want one", warnings)
	}
	want := "Skipped '" + notDir + "' path due to this access issue: '" + notDir + "' is not a directory\n"
	if warnings[0] != want {
		t.Errorf("warning = %q\nwant %q", warnings[0], want)
	}
	// And a run with nothing skipped reports no warnings key at all.
	res, err = moduleFind(ctx, conn, map[string]any{"paths": root})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := res.Extra["warnings"]; present {
		t.Error("warnings reported with nothing skipped")
	}
	if s, _ := res.Extra["skipped_paths"].(map[string]any); len(s) != 0 {
		t.Errorf("skipped_paths = %#v, want empty", s)
	}
}
