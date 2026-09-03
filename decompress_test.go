package modules

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecompressDefaultDest(t *testing.T) {
	if got, want := decompressDefaultDest("/a/f.txt.gz", "gz"), "/a/f.txt"; got != want {
		t.Errorf("decompressDefaultDest = %q, want %q", got, want)
	}
	if got, want := decompressDefaultDest("/a/f.myext", "gz"), "/a/f.myext_decompressed"; got != want {
		t.Errorf("decompressDefaultDest = %q, want %q", got, want)
	}
}

func TestDecompressCmd(t *testing.T) {
	cases := map[string]string{
		"gz":  "gunzip -c /a/f.gz > /a/f",
		"bz2": "bunzip2 -c /a/f.gz > /a/f",
		"xz":  "xz -dc /a/f.gz > /a/f",
	}
	for format, want := range cases {
		if got := decompressCmd("/a/f.gz", "/a/f", format); got != want {
			t.Errorf("decompressCmd(%q) = %q, want %q", format, got, want)
		}
	}
}

func TestModuleDecompressGz(t *testing.T) {
	if _, err := exec.LookPath("gzip"); err != nil {
		t.Skip("gzip not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "f.txt.gz")
	if err := runShellForTest(t, "gzip -c > "+src, "hello world"); err != nil {
		t.Fatal(err)
	}

	conn := local()
	res, err := moduleDecompress(context.Background(), conn, map[string]any{"src": src})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	dest := filepath.Join(dir, "f.txt")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("content = %q", data)
	}

	// Re-running against the already-correct dest must report unchanged.
	res2, err := moduleDecompress(context.Background(), conn, map[string]any{"src": src})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged on second run")
	}
}

func TestModuleDecompressRemove(t *testing.T) {
	if _, err := exec.LookPath("gzip"); err != nil {
		t.Skip("gzip not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "f.txt.gz")
	if err := runShellForTest(t, "gzip -c > "+src, "data"); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleDecompress(context.Background(), conn, map[string]any{"src": src, "remove": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("want src removed, stat err = %v", err)
	}
}

func TestModuleDecompressMissingSrc(t *testing.T) {
	conn := local()
	if _, err := moduleDecompress(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing src")
	}
}

func TestModuleDecompressInvalidFormat(t *testing.T) {
	conn := local()
	if _, err := moduleDecompress(context.Background(), conn, map[string]any{
		"src": "/a/f", "format": "zstd",
	}); err == nil {
		t.Fatal("want error for invalid format")
	}
}

// runShellForTest pipes stdin through a shell command, used to produce
// fixture files with real gzip/bzip2/etc without adding a build-time
// dependency on any particular compression package.
func runShellForTest(t *testing.T, cmd, stdin string) error {
	t.Helper()
	c := exec.Command("sh", "-c", cmd)
	c.Stdin = strings.NewReader(stdin)
	return c.Run()
}
