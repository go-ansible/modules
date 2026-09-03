package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleIniFileAddOptionNewSection(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.ini")
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "section": "drinks", "option": "fav", "value": "lemonade",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "[drinks]\nfav = lemonade\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}

	// Re-running must be a no-op.
	res2, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "section": "drinks", "option": "fav", "value": "lemonade",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleIniFileReplaceExistingOption(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.ini")
	if err := os.WriteFile(f, []byte("[drinks]\nfav = coffee\nother = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "section": "drinks", "option": "fav", "value": "lemonade",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "[drinks]\nfav = lemonade\nother = x\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleIniFileNonExclusiveAddsMultipleValues(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.ini")
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "section": "drinks", "option": "beverage",
		"values": []string{"coke", "pepsi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "[drinks]\nbeverage = coke\nbeverage = pepsi\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleIniFileOptionOutsideSection(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.ini")
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "option": "beverage", "value": "lemon juice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "beverage = lemon juice\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleIniFileAbsentOption(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.ini")
	if err := os.WriteFile(f, []byte("[drinks]\nfav = coffee\nother = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "section": "drinks", "option": "fav", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "[drinks]\nother = x\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleIniFileAbsentSection(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.ini")
	if err := os.WriteFile(f, []byte("[a]\nx = 1\n[b]\ny = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "section": "a", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "[b]\ny = 2\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleIniFileCreateFalseFailsWhenMissing(t *testing.T) {
	f := filepath.Join(t.TempDir(), "absent.ini")
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "option": "x", "value": "y", "create": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: file absent and create=false")
	}
}

func TestModuleIniFileNoExtraSpaces(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.ini")
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "option": "x", "value": "y", "no_extra_spaces": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "x=y\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleIniFileMissingPath(t *testing.T) {
	conn := local()
	if _, err := moduleIniFile(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
}

func TestModuleIniFilePresentRequiresValue(t *testing.T) {
	conn := local()
	f := filepath.Join(t.TempDir(), "f.ini")
	if _, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "option": "x",
	}); err == nil {
		t.Fatal("want error: value required for state=present without allow_no_value")
	}
}

func TestModuleIniFileAllowNoValue(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.ini")
	conn := local()
	res, err := moduleIniFile(context.Background(), conn, map[string]any{
		"path": f, "option": "flag", "allow_no_value": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "flag\n" {
		t.Fatalf("content = %q", data)
	}
}
