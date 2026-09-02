package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleFetchNew(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "remote.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "nested", "local.txt")
	conn := local()

	res, err := moduleFetch(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleFetchUnchangedWhenIdentical(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "remote.txt")
	dest := filepath.Join(dir, "local.txt")
	if err := os.WriteFile(src, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()

	res, err := moduleFetch(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when dest already matches src")
	}
}

func TestModuleFetchChangedWhenDifferent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "remote.txt")
	dest := filepath.Join(dir, "local.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()

	res, err := moduleFetch(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed when dest differs from src")
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "new" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleFetchMissingSrcFails(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleFetch(context.Background(), conn, map[string]any{
		"src": filepath.Join(dir, "absent"), "dest": filepath.Join(dir, "out"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed by default when src is missing")
	}
}

func TestModuleFetchMissingSrcNoFail(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleFetch(context.Background(), conn, map[string]any{
		"src": filepath.Join(dir, "absent"), "dest": filepath.Join(dir, "out"), "fail_on_missing": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want not Failed when fail_on_missing is false")
	}
}

func TestModuleFetchMissingArgs(t *testing.T) {
	conn := local()
	if _, err := moduleFetch(context.Background(), conn, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error for missing src")
	}
	if _, err := moduleFetch(context.Background(), conn, map[string]any{"src": "/x"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}

func TestModuleFetchSrcIsDirectoryFails(t *testing.T) {
	// src exists (pathExists is true for a directory too) but the
	// underlying Fetch/copyFile fails trying to read it as a file —
	// exercises the "conn.Fetch itself failed" branch distinct from
	// "src does not exist".
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "adir")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	conn := local()
	if _, err := moduleFetch(context.Background(), conn, map[string]any{
		"src": srcDir, "dest": filepath.Join(dir, "out"),
	}); err == nil {
		t.Fatal("want error when src is a directory")
	}
}

func TestModuleFetchMkdirFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "remote.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	if _, err := moduleFetch(context.Background(), conn, map[string]any{
		"src": src, "dest": filepath.Join(blocker, "sub", "out"),
	}); err == nil {
		t.Fatal("want error when dest's parent path is blocked by a regular file")
	}
}
