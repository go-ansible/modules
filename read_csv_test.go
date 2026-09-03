package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleReadCsvWithHeaderAndKey(t *testing.T) {
	f := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(f, []byte("name,uid,gid\ndag,500,500\njeroen,501,500\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleReadCsv(context.Background(), conn, map[string]any{"path": f, "key": "name"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, want a plain Ok", res)
	}
	list, ok := res.Extra["list"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("list = %v", res.Extra["list"])
	}
	dict, ok := res.Extra["dict"].(map[string]any)
	if !ok {
		t.Fatalf("dict = %v", res.Extra["dict"])
	}
	dag, ok := dict["dag"].(map[string]any)
	if !ok || dag["uid"] != "500" {
		t.Fatalf("dict[dag] = %v", dict["dag"])
	}
}

func TestModuleReadCsvNoHeaderWithFieldnames(t *testing.T) {
	f := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(f, []byte("dag;500;500\njeroen;501;500\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleReadCsv(context.Background(), conn, map[string]any{
		"path": f, "fieldnames": []string{"name", "uid", "gid"}, "delimiter": ";",
	})
	if err != nil {
		t.Fatal(err)
	}
	list := res.Extra["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("list = %v", list)
	}
	first := list[0].(map[string]any)
	if first["name"] != "dag" || first["uid"] != "500" {
		t.Fatalf("first row = %v", first)
	}
}

func TestModuleReadCsvExcelTabDialect(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.csv")
	if err := os.WriteFile(f, []byte("a\tb\n1\t2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleReadCsv(context.Background(), conn, map[string]any{"path": f, "dialect": "excel-tab"})
	if err != nil {
		t.Fatal(err)
	}
	list := res.Extra["list"].([]any)
	row := list[0].(map[string]any)
	if row["a"] != "1" || row["b"] != "2" {
		t.Fatalf("row = %v", row)
	}
}

func TestModuleReadCsvRaggedRowsDoNotFail(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.csv")
	if err := os.WriteFile(f, []byte("a,b,c\n1,2\n3,4,5,6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleReadCsv(context.Background(), conn, map[string]any{"path": f})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("want success despite ragged rows, res = %+v", res)
	}
	list := res.Extra["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("list = %v", list)
	}
	short := list[0].(map[string]any)
	if _, ok := short["c"]; ok {
		t.Fatalf("short row should have no 'c' key: %v", short)
	}
}

func TestModuleReadCsvDuplicateKeyFails(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.csv")
	if err := os.WriteFile(f, []byte("name,uid\ndag,500\ndag,501\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleReadCsv(context.Background(), conn, map[string]any{"path": f, "key": "name"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: duplicate key with unique=true (default)")
	}
}

func TestModuleReadCsvDuplicateKeyAllowedWhenNotUnique(t *testing.T) {
	f := filepath.Join(t.TempDir(), "t.csv")
	if err := os.WriteFile(f, []byte("name,uid\ndag,500\ndag,501\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleReadCsv(context.Background(), conn, map[string]any{"path": f, "key": "name", "unique": false})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("want success: unique=false, res = %+v", res)
	}
}

func TestModuleReadCsvMissingFile(t *testing.T) {
	conn := local()
	res, err := moduleReadCsv(context.Background(), conn, map[string]any{
		"path": filepath.Join(t.TempDir(), "absent.csv"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: file does not exist")
	}
}

func TestModuleReadCsvMissingPath(t *testing.T) {
	conn := local()
	if _, err := moduleReadCsv(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
}
