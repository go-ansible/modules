package modules

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleSlurp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "f.bin")
	payload := []byte{0x00, 0x01, 0xff, 'h', 'i'}
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()

	res, err := moduleSlurp(context.Background(), conn, map[string]any{"src": src})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["encoding"] != "base64" {
		t.Fatalf("encoding = %v", res.Extra["encoding"])
	}
	got, err := base64.StdEncoding.DecodeString(res.Extra["content"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("content = %v, want %v", got, payload)
	}
}

func TestModuleSlurpPathAlias(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleSlurp(context.Background(), conn, map[string]any{"path": src})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSlurpMissingSrc(t *testing.T) {
	conn := local()
	if _, err := moduleSlurp(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing src")
	}
}

func TestModuleSlurpNotFound(t *testing.T) {
	conn := local()
	res, err := moduleSlurp(context.Background(), conn, map[string]any{"src": filepath.Join(t.TempDir(), "absent")})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a missing remote file")
	}
}

func TestModuleSlurpSrcIsDirectoryFails(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "adir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	conn := local()
	if _, err := moduleSlurp(context.Background(), conn, map[string]any{"src": sub}); err == nil {
		t.Fatal("want error when src is a directory")
	}
}
