package modules

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestArchiveCompressCmd(t *testing.T) {
	got := archiveCompressCmd("/a/f", "/a/f.gz", "gz")
	want := "gzip -c /a/f > /a/f.gz"
	if got != want {
		t.Errorf("archiveCompressCmd = %q, want %q", got, want)
	}
}

func TestArchiveTarCmd(t *testing.T) {
	got := archiveTarCmd([]string{"/a/foo", "/b/bar"}, "/out.tar.gz", "gz")
	want := "tar czf /out.tar.gz -C /a foo -C /b bar"
	if got != want {
		t.Errorf("archiveTarCmd = %q, want %q", got, want)
	}
}

func TestArchiveZipCmd(t *testing.T) {
	got := archiveZipCmd([]string{"/a/foo"}, "/out.zip")
	want := "(cd /a && zip -r /out.zip foo)"
	if got != want {
		t.Errorf("archiveZipCmd = %q, want %q", got, want)
	}
}

func TestModuleArchiveCompressSingleFile(t *testing.T) {
	if _, err := exec.LookPath("gzip"); err != nil {
		t.Skip("gzip not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleArchive(context.Background(), conn, map[string]any{"path": []string{src}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if _, err := os.Stat(src + ".gz"); err != nil {
		t.Fatalf("expected %s.gz to exist: %v", src, err)
	}
}

func TestModuleArchiveMultiPathTar(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not available")
	}
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "foo"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "bar"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.tar")
	conn := local()
	res, err := moduleArchive(context.Background(), conn, map[string]any{
		"path": []string{filepath.Join(a, "foo"), filepath.Join(b, "bar")},
		"dest": dest, "format": "tar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected archive at %s: %v", dest, err)
	}
}

func TestModuleArchiveSkipsWhenDestExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote("/out.tar.gz"): {RC: 0},
	})
	res, err := moduleArchive(context.Background(), conn, map[string]any{
		"path": []string{"/src"}, "dest": "/out.tar.gz",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: dest already exists and force not set")
	}
}

func TestModuleArchiveForceOverwrites(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote("/out.tar.gz"): {RC: 0},
		"gzip -c /src > /out.tar.gz":           {RC: 0},
	})
	res, err := moduleArchive(context.Background(), conn, map[string]any{
		"path": []string{"/src"}, "dest": "/out.tar.gz", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: force set")
	}
}

func TestModuleArchiveMissingPath(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleArchive(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
}

func TestModuleArchiveInvalidFormat(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleArchive(context.Background(), conn, map[string]any{
		"path": []string{"/src"}, "dest": "/out", "format": "rar",
	}); err == nil {
		t.Fatal("want error for invalid format")
	}
}

func TestModuleArchiveMultiPathRequiresDest(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleArchive(context.Background(), conn, map[string]any{
		"path": []string{"/a", "/b"},
	}); err == nil {
		t.Fatal("want error: dest required for multi-path archive")
	}
}
