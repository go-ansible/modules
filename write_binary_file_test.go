package modules

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleWriteBinaryFileCreates(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	content := []byte{0x00, 0x01, 0xFF, 0xFE, 'h', 'i'}
	b64 := base64.StdEncoding.EncodeToString(content)

	conn := local()
	res, err := moduleWriteBinaryFile(context.Background(), conn, map[string]any{
		"path": dest, "content": b64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %v, want %v", got, content)
	}
}

func TestModuleWriteBinaryFileIdempotent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	content := []byte{1, 2, 3}
	b64 := base64.StdEncoding.EncodeToString(content)

	conn := local()
	if _, err := moduleWriteBinaryFile(context.Background(), conn, map[string]any{"path": dest, "content": b64}); err != nil {
		t.Fatal(err)
	}
	res, err := moduleWriteBinaryFile(context.Background(), conn, map[string]any{"path": dest, "content": b64})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged on second identical write")
	}
}

func TestModuleWriteBinaryFileForceFalseSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(dest, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	newContent := base64.StdEncoding.EncodeToString([]byte("new"))

	conn := local()
	res, err := moduleWriteBinaryFile(context.Background(), conn, map[string]any{
		"path": dest, "content": newContent, "force": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when force=false and destination exists")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("content = %q, want untouched", got)
	}
}

func TestModuleWriteBinaryFileBackup(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(dest, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	newContent := base64.StdEncoding.EncodeToString([]byte("new"))

	conn := local()
	res, err := moduleWriteBinaryFile(context.Background(), conn, map[string]any{
		"path": dest, "content": newContent, "backup": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	backupFile, ok := res.Extra["backup_file"].(string)
	if !ok || backupFile == "" {
		t.Fatalf("want backup_file set, extra = %+v", res.Extra)
	}
	got, err := os.ReadFile(backupFile)
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("backup content = %q, want original", got)
	}
}

func TestModuleWriteBinaryFileMode(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	content := base64.StdEncoding.EncodeToString([]byte("x"))

	conn := local()
	res, err := moduleWriteBinaryFile(context.Background(), conn, map[string]any{
		"path": dest, "content": content, "mode": "0600",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestModuleWriteBinaryFileInvalidBase64(t *testing.T) {
	conn := local()
	if _, err := moduleWriteBinaryFile(context.Background(), conn, map[string]any{
		"path": filepath.Join(t.TempDir(), "x"), "content": "not-valid-base64!!!",
	}); err == nil {
		t.Fatal("want error for invalid base64")
	}
}

func TestModuleWriteBinaryFileMissingArgs(t *testing.T) {
	conn := local()
	if _, err := moduleWriteBinaryFile(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path/content")
	}
}
