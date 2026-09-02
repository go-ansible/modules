package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleFileDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "deep")
	conn := local()
	res, err := moduleFile(context.Background(), conn, map[string]any{"path": dir, "state": "directory"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed on first create")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}

	res2, err := moduleFile(context.Background(), conn, map[string]any{"path": dir, "state": "directory"})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged on second call")
	}
}

func TestModuleFileAbsent(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	if err := os.WriteFile(f, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleFile(context.Background(), conn, map[string]any{"path": f, "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("file still exists")
	}

	res2, err := moduleFile(context.Background(), conn, map[string]any{"path": f, "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged when already absent")
	}
}

func TestModuleFileTouch(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x")
	conn := local()
	res, err := moduleFile(context.Background(), conn, map[string]any{"path": f, "state": "touch"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed on first touch")
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal(err)
	}
}

func TestModuleFileStateFileMissing(t *testing.T) {
	f := filepath.Join(t.TempDir(), "absent")
	conn := local()
	res, err := moduleFile(context.Background(), conn, map[string]any{"path": f, "state": "file"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: state=file must not create files")
	}
}

func TestModuleFileMode(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(f, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleFile(context.Background(), conn, map[string]any{"path": f, "mode": "0600"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestModuleFileLink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleFile(context.Background(), conn, map[string]any{"path": link, "state": "link", "src": src})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	target, err := os.Readlink(link)
	if err != nil || target != src {
		t.Fatalf("link target = %q err=%v", target, err)
	}

	res2, err := moduleFile(context.Background(), conn, map[string]any{"path": link, "state": "link", "src": src})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged when link already correct")
	}
}

func TestModuleFileUnknownState(t *testing.T) {
	conn := local()
	if _, err := moduleFile(context.Background(), conn, map[string]any{"path": "/x", "state": "bogus"}); err == nil {
		t.Fatal("want error for unknown state")
	}
}
