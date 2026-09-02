package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleBlockinfileInsertNewAtEOF(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "line1\nline2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "existing\n# BEGIN ANSIBLE MANAGED BLOCK\nline1\nline2\n# END ANSIBLE MANAGED BLOCK\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleBlockinfileIdempotent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	conn := local()
	if _, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "line1\nline2", "create": true,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "line1\nline2", "create": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged on second identical run")
	}
}

func TestModuleBlockinfileUpdateInPlace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	initial := "before\n# BEGIN ANSIBLE MANAGED BLOCK\nold content\n# END ANSIBLE MANAGED BLOCK\nafter\n"
	if err := os.WriteFile(f, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "new content",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "before\n# BEGIN ANSIBLE MANAGED BLOCK\nnew content\n# END ANSIBLE MANAGED BLOCK\nafter\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleBlockinfileEmptyBlockRemoves(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	initial := "before\n# BEGIN ANSIBLE MANAGED BLOCK\nold content\n# END ANSIBLE MANAGED BLOCK\nafter\n"
	if err := os.WriteFile(f, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "before\nafter\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleBlockinfileStateAbsent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	initial := "before\n# BEGIN ANSIBLE MANAGED BLOCK\nold content\n# END ANSIBLE MANAGED BLOCK\nafter\n"
	if err := os.WriteFile(f, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "before\nafter\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleBlockinfileStateAbsentNoBlock(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: no block to remove")
	}
}

func TestModuleBlockinfileMissingFileNoCreate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "absent.txt")
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: file absent and create not set")
	}
}

func TestModuleBlockinfileMissingFileAbsentIsNoop(t *testing.T) {
	f := filepath.Join(t.TempDir(), "absent.txt")
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want a no-op ok", res)
	}
}

func TestModuleBlockinfileCreate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "new.txt")
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "hello", "create": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "# BEGIN ANSIBLE MANAGED BLOCK\nhello\n# END ANSIBLE MANAGED BLOCK\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleBlockinfileInsertAfterRegexp(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("first\nanchor\nlast\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "inserted", "insertafter": "^anchor$",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "first\nanchor\n# BEGIN ANSIBLE MANAGED BLOCK\ninserted\n# END ANSIBLE MANAGED BLOCK\nlast\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleBlockinfileInsertBeforeRegexp(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("first\nanchor\nlast\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "inserted", "insertbefore": "^anchor$",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "first\n# BEGIN ANSIBLE MANAGED BLOCK\ninserted\n# END ANSIBLE MANAGED BLOCK\nanchor\nlast\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleBlockinfileInsertBeforeBOF(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "inserted", "insertbefore": "BOF",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "# BEGIN ANSIBLE MANAGED BLOCK\ninserted\n# END ANSIBLE MANAGED BLOCK\nfirst\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleBlockinfileInsertAfterNoMatchAppendsEOF(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "inserted", "insertafter": "^nomatch$",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "first\n# BEGIN ANSIBLE MANAGED BLOCK\ninserted\n# END ANSIBLE MANAGED BLOCK\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleBlockinfileInvalidInsertafterRegexp(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	if _, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "y", "insertafter": "(unclosed",
	}); err == nil {
		t.Fatal("want error for invalid regexp")
	}
}

func TestModuleBlockinfileInvalidInsertbeforeRegexp(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	if _, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "y", "insertbefore": "(unclosed",
	}); err == nil {
		t.Fatal("want error for invalid regexp")
	}
}

func TestModuleBlockinfileInvalidState(t *testing.T) {
	conn := local()
	if _, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": "/x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleBlockinfileCustomMarker(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	conn := local()
	res, err := moduleBlockinfile(context.Background(), conn, map[string]any{
		"path": f, "block": "x", "create": true, "marker": "// {mark} CUSTOM",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "// BEGIN CUSTOM\nx\n// END CUSTOM\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleBlockinfileMissingPath(t *testing.T) {
	conn := local()
	if _, err := moduleBlockinfile(context.Background(), conn, map[string]any{"block": "x"}); err == nil {
		t.Fatal("want error")
	}
}

func TestFindMarkerBlockNoEnd(t *testing.T) {
	begin, end := findMarkerBlock([]string{"# BEGIN ANSIBLE MANAGED BLOCK", "x"}, "# BEGIN ANSIBLE MANAGED BLOCK", "# END ANSIBLE MANAGED BLOCK")
	if begin != -1 || end != -1 {
		t.Fatalf("begin=%d end=%d, want -1,-1", begin, end)
	}
}
