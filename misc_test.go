package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleStat(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleStat(context.Background(), conn, map[string]any{"path": f})
	if err != nil {
		t.Fatal(err)
	}
	stat := res.Extra["stat"].(map[string]any)
	if stat["exists"] != true {
		t.Fatalf("exists = %v", stat["exists"])
	}
	if stat["size"] != int64(5) {
		t.Fatalf("size = %v", stat["size"])
	}
	if stat["isreg"] != true {
		t.Fatalf("isreg = %v", stat["isreg"])
	}
}

func TestModuleStatAbsent(t *testing.T) {
	conn := local()
	res, err := moduleStat(context.Background(), conn, map[string]any{"path": filepath.Join(t.TempDir(), "absent")})
	if err != nil {
		t.Fatal(err)
	}
	stat := res.Extra["stat"].(map[string]any)
	if stat["exists"] != false {
		t.Fatalf("exists = %v, want false", stat["exists"])
	}
}

func TestModuleStatDir(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleStat(context.Background(), conn, map[string]any{"path": dir})
	if err != nil {
		t.Fatal(err)
	}
	stat := res.Extra["stat"].(map[string]any)
	if stat["isdir"] != true {
		t.Fatalf("isdir = %v", stat["isdir"])
	}
}

func TestModuleDebugMsg(t *testing.T) {
	conn := local()
	res, err := moduleDebug(context.Background(), conn, map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "hi" || res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDebugDefault(t *testing.T) {
	conn := local()
	res, err := moduleDebug(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "Hello world!" {
		t.Fatalf("Msg = %q", res.Msg)
	}
}

func TestModuleDebugVar(t *testing.T) {
	conn := local()
	res, err := moduleDebug(context.Background(), conn, map[string]any{"var": 42})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "42" {
		t.Fatalf("Msg = %q", res.Msg)
	}
}

func TestModuleFail(t *testing.T) {
	conn := local()
	res, err := moduleFail(context.Background(), conn, map[string]any{"msg": "boom"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "boom" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleFailDefault(t *testing.T) {
	conn := local()
	res, err := moduleFail(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}

func TestModuleAssertPass(t *testing.T) {
	conn := local()
	res, err := moduleAssert(context.Background(), conn, map[string]any{"that": []any{true, true}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want success")
	}
}

func TestModuleAssertFail(t *testing.T) {
	conn := local()
	res, err := moduleAssert(context.Background(), conn, map[string]any{
		"that": []any{true, false}, "fail_msg": "nope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "nope" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAssertSingleBool(t *testing.T) {
	conn := local()
	res, err := moduleAssert(context.Background(), conn, map[string]any{"that": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want success")
	}
}

func TestModuleAssertMissingThat(t *testing.T) {
	conn := local()
	if _, err := moduleAssert(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing that")
	}
}

func TestModuleAssertNonBoolCondition(t *testing.T) {
	conn := local()
	if _, err := moduleAssert(context.Background(), conn, map[string]any{"that": []any{"not a bool"}}); err == nil {
		t.Fatal("want error for non-bool condition")
	}
}

func TestModuleSetFact(t *testing.T) {
	conn := local()
	res, err := moduleSetFact(context.Background(), conn, map[string]any{"x": 1, "y": "z"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("set_fact must never report changed")
	}
	if res.Facts["x"] != 1 || res.Facts["y"] != "z" {
		t.Fatalf("Facts = %v", res.Facts)
	}
}

func TestModuleTemplate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "t.j2")
	if err := os.WriteFile(src, []byte("hello {{ name }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.txt")
	conn := local()
	res, err := moduleTemplate(context.Background(), conn, map[string]any{
		"src": src, "dest": dest, "_vars": map[string]any{"name": "world"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world\n" {
		t.Fatalf("content = %q", data)
	}

	res2, err := moduleTemplate(context.Background(), conn, map[string]any{
		"src": src, "dest": dest, "_vars": map[string]any{"name": "world"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged on identical re-render")
	}
}

func TestModuleTemplateMissingSrc(t *testing.T) {
	conn := local()
	_, err := moduleTemplate(context.Background(), conn, map[string]any{
		"src": filepath.Join(t.TempDir(), "absent.j2"), "dest": "/x",
	})
	if err == nil {
		t.Fatal("want error for unreadable src")
	}
}
