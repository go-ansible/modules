package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleCopyContent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	conn := local()

	res, err := moduleCopy(context.Background(), conn, map[string]any{"content": "hello", "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed on first write")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}

	res2, err := moduleCopy(context.Background(), conn, map[string]any{"content": "hello", "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged on identical re-copy")
	}
}

func TestModuleCopySrc(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dest := filepath.Join(dir, "dest.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCopy(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleCopyMode(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	conn := local()
	_, err := moduleCopy(context.Background(), conn, map[string]any{"content": "x", "dest": dest, "mode": "0600"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestModuleCopyMkdirParents(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "nested", "deeper", "out.txt")
	conn := local()
	_, err := moduleCopy(context.Background(), conn, map[string]any{"content": "x", "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
}

func TestModuleCopyMissingSrcAndContent(t *testing.T) {
	conn := local()
	if _, err := moduleCopy(context.Background(), conn, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error when neither content nor src is given")
	}
}

func TestModuleCopySrcReadError(t *testing.T) {
	conn := local()
	_, err := moduleCopy(context.Background(), conn, map[string]any{
		"src": filepath.Join(t.TempDir(), "absent"), "dest": "/x",
	})
	if err == nil {
		t.Fatal("want error for unreadable src")
	}
}
