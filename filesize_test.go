package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesizeParseBytes(t *testing.T) {
	cases := []struct {
		in   string
		bs   int64
		want int64
	}{
		{"1G", 512, 1073741824},
		{"2GB", 512, 2000000000},
		{"512B", 512, 512},
		{"4", 512, 2048}, // no suffix: 4 blocks of 512 bytes
		{"1MiB", 512, 1048576},
		{"1K", 512, 1024},
		{"1KB", 512, 1000},
	}
	for _, c := range cases {
		got, err := filesizeParseBytes(c.in, c.bs)
		if err != nil {
			t.Errorf("filesizeParseBytes(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("filesizeParseBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFilesizeParseBytesInvalid(t *testing.T) {
	if _, err := filesizeParseBytes("banana", 512); err == nil {
		t.Fatal("want error for non-numeric size")
	}
	if _, err := filesizeParseBytes("5XB", 512); err == nil {
		t.Fatal("want error for unrecognized unit")
	}
}

func TestModuleFilesizeCreateAndGrow(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bigfile")
	conn := local()

	res, err := moduleFilesize(context.Background(), conn, map[string]any{"path": f, "size": "1024B"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: file created")
	}
	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 1024 {
		t.Fatalf("size = %d, want 1024", info.Size())
	}

	// Re-running with the same size must be a no-op.
	res2, err := moduleFilesize(context.Background(), conn, map[string]any{"path": f, "size": "1024B"})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged: size already matches")
	}

	// Grow: existing bytes must be preserved.
	if err := os.WriteFile(f, []byte("abcd"), 0o644); err != nil {
		t.Fatal(err)
	}
	res3, err := moduleFilesize(context.Background(), conn, map[string]any{"path": f, "size": "8B"})
	if err != nil {
		t.Fatal(err)
	}
	if !res3.Changed {
		t.Fatal("want changed: grown")
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 8 || string(data[:4]) != "abcd" {
		t.Fatalf("content = %q, want first 4 bytes preserved", data)
	}
}

func TestModuleFilesizeShrink(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("abcdefgh"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleFilesize(context.Background(), conn, map[string]any{"path": f, "size": "4B"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: shrunk")
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcd" {
		t.Fatalf("content = %q, want %q", data, "abcd")
	}
}

func TestModuleFilesizeSparse(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "sparse")
	conn := local()
	res, err := moduleFilesize(context.Background(), conn, map[string]any{
		"path": f, "size": "1MB", "sparse": true,
	})
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
	if info.Size() != 1000000 {
		t.Fatalf("size = %d, want 1000000", info.Size())
	}
}

func TestModuleFilesizeForceAlwaysChanged(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("xxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleFilesize(context.Background(), conn, map[string]any{
		"path": f, "size": "4B", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: force always reports changed")
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range data {
		if b != 0 {
			t.Fatalf("content = %q, want all-zero bytes after force truncate+refill", data)
		}
	}
}

func TestModuleFilesizeSparseAndForceMutuallyExclusive(t *testing.T) {
	conn := local()
	if _, err := moduleFilesize(context.Background(), conn, map[string]any{
		"path": "/x", "size": "1B", "sparse": true, "force": true,
	}); err == nil {
		t.Fatal("want error: sparse and force mutually exclusive")
	}
}

func TestModuleFilesizeMissingArgs(t *testing.T) {
	conn := local()
	if _, err := moduleFilesize(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
	if _, err := moduleFilesize(context.Background(), conn, map[string]any{"path": "/x"}); err == nil {
		t.Fatal("want error for missing size")
	}
}
