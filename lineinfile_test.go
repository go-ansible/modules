package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleLineinfileAppend(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleLineinfile(context.Background(), conn, map[string]any{"path": f, "line": "c"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "a\nb\nc\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleLineinfileAlreadyPresent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleLineinfile(context.Background(), conn, map[string]any{"path": f, "line": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: line already present")
	}
}

func TestModuleLineinfileRegexpReplace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("Port 22\nOther x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleLineinfile(context.Background(), conn, map[string]any{
		"path": f, "regexp": "^Port ", "line": "Port 2222",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "Port 2222\nOther x\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleLineinfileAbsent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleLineinfile(context.Background(), conn, map[string]any{"path": f, "line": "b", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "a\nc\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleLineinfileCreate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "new.txt")
	conn := local()
	res, err := moduleLineinfile(context.Background(), conn, map[string]any{"path": f, "line": "x", "create": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "x\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleLineinfileMissingNoCreate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "absent.txt")
	conn := local()
	res, err := moduleLineinfile(context.Background(), conn, map[string]any{"path": f, "line": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: file absent and create not set")
	}
}

func TestModuleReplace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("foo123 bar456"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleReplace(context.Background(), conn, map[string]any{
		"path": f, "regexp": `\d+`, "replace": "N",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "fooN barN" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleReplaceUnchanged(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("no digits here"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleReplace(context.Background(), conn, map[string]any{
		"path": f, "regexp": `\d+`, "replace": "N",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: no match")
	}
}

func TestModuleReplaceMissingFile(t *testing.T) {
	conn := local()
	res, err := moduleReplace(context.Background(), conn, map[string]any{
		"path": filepath.Join(t.TempDir(), "absent"), "regexp": "x", "replace": "y",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}
