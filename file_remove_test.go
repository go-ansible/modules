package modules

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeFileForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModuleFileRemoveGlobNonRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFileForTest(t, filepath.Join(dir, "a.log"), "a")
	writeFileForTest(t, filepath.Join(dir, "b.log"), "b")
	writeFileForTest(t, filepath.Join(dir, "keep.txt"), "c")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileForTest(t, filepath.Join(dir, "sub", "c.log"), "d")

	conn := local()
	res, err := moduleFileRemove(context.Background(), conn, map[string]any{"path": dir, "pattern": "*.log"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if res.Extra["files_count"] != 2 {
		t.Fatalf("files_count = %v", res.Extra["files_count"])
	}
	for _, name := range []string{"a.log", "b.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should have been removed", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("keep.txt should still exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "c.log")); err != nil {
		t.Fatalf("sub/c.log should NOT have been removed (non-recursive): %v", err)
	}
}

func TestModuleFileRemoveRecursive(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileForTest(t, filepath.Join(dir, "top.tmp"), "a")
	writeFileForTest(t, filepath.Join(dir, "sub", "nested.tmp"), "b")

	conn := local()
	res, err := moduleFileRemove(context.Background(), conn, map[string]any{"path": dir, "pattern": "*.tmp", "recursive": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	removed, _ := res.Extra["removed_files"].([]string)
	sort.Strings(removed)
	if len(removed) != 2 {
		t.Fatalf("removed_files = %v", removed)
	}
}

func TestModuleFileRemoveRegex(t *testing.T) {
	dir := t.TempDir()
	writeFileForTest(t, filepath.Join(dir, "backup_20240101.tar.gz"), "a")
	writeFileForTest(t, filepath.Join(dir, "notes.txt"), "b")

	conn := local()
	res, err := moduleFileRemove(context.Background(), conn, map[string]any{
		"path": dir, "pattern": `backup_[0-9]{8}\.tar\.gz`, "use_regex": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "backup_20240101.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("backup archive should have been removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatalf("notes.txt should still exist: %v", err)
	}
}

func TestModuleFileRemoveNoMatch(t *testing.T) {
	dir := t.TempDir()
	writeFileForTest(t, filepath.Join(dir, "keep.txt"), "a")

	conn := local()
	res, err := moduleFileRemove(context.Background(), conn, map[string]any{"path": dir, "pattern": "*.log"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if res.Extra["files_count"] != 0 {
		t.Fatalf("files_count = %v", res.Extra["files_count"])
	}
}

func TestModuleFileRemoveInvalidRegex(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	_, err := moduleFileRemove(context.Background(), conn, map[string]any{"path": dir, "pattern": "[", "use_regex": true})
	if err == nil {
		t.Fatal("want error for invalid regex")
	}
}

func TestModuleFileRemoveMissingPath(t *testing.T) {
	conn := local()
	_, err := moduleFileRemove(context.Background(), conn, map[string]any{"path": "/does/not/exist/at/all", "pattern": "*"})
	if err == nil {
		t.Fatal("want error for missing path")
	}
}

func TestModuleFileRemoveNegatedGlob(t *testing.T) {
	dir := t.TempDir()
	writeFileForTest(t, filepath.Join(dir, "a.txt"), "a")
	writeFileForTest(t, filepath.Join(dir, "b.txt"), "b")
	writeFileForTest(t, filepath.Join(dir, "a.log"), "c")

	conn := local()
	res, err := moduleFileRemove(context.Background(), conn, map[string]any{"path": dir, "pattern": "[!a].txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("b.txt should have been removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("a.txt should still exist: %v", err)
	}
}
