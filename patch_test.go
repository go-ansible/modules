package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModulePatchApplyInvalidPatchFails(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "index.html")
	if err := os.WriteFile(dest, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	patchFile := filepath.Join(dir, "index.html.patch")
	if err := os.WriteFile(patchFile, []byte("not a real patch"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn := local()
	res, err := modulePatch(context.Background(), conn, map[string]any{
		"src": patchFile, "dest": dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A malformed patch file makes real `patch` exit non-zero; that's a
	// clean Result{Failed:true}, not a Go error (see modulePatch's
	// non-zero-exit handling).
	if !res.Failed {
		t.Fatalf("want a clean Fail result for an invalid patch, got %+v", res)
	}
}

func TestModulePatchCmdDestForm(t *testing.T) {
	got := patchCmd("/tmp/x.patch", "/var/www/index.html", "", 1, false, false, false)
	want := "patch -N -p1 /var/www/index.html < /tmp/x.patch"
	if got != want {
		t.Fatalf("patchCmd = %q, want %q", got, want)
	}
}

func TestModulePatchCmdBasedirForm(t *testing.T) {
	got := patchCmd("/tmp/x.patch", "", "/var/www", 1, true, true, true)
	want := "cd /var/www && patch -N -p1 -R --backup --version-control=numbered --ignore-whitespace < /tmp/x.patch"
	if got != want {
		t.Fatalf("patchCmd = %q, want %q", got, want)
	}
}

func TestModulePatchValidation(t *testing.T) {
	conn := local()
	if _, err := modulePatch(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing src")
	}
	if _, err := modulePatch(context.Background(), conn, map[string]any{"src": "/tmp/x.patch"}); err == nil {
		t.Fatal("want error for missing dest and basedir")
	}
	if _, err := modulePatch(context.Background(), conn, map[string]any{
		"src": "/tmp/x.patch", "dest": "/x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for bad state")
	}
}

func TestModulePatchRealApply(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "greeting.txt")
	if err := os.WriteFile(dest, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patchFile := filepath.Join(dir, "greeting.patch")
	diff := "--- a/greeting.txt\n+++ b/greeting.txt\n@@ -1 +1 @@\n-hello\n+goodbye\n"
	if err := os.WriteFile(patchFile, []byte(diff), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := modulePatch(context.Background(), conn, map[string]any{
		"src": patchFile, "dest": dest, "strip": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, got %+v", res)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "goodbye\n" {
		t.Fatalf("dest content = %q", data)
	}

	// Re-applying should now be a no-op thanks to -N (see modulePatch's
	// doc comment on why -N is always added).
	res2, err := modulePatch(context.Background(), conn, map[string]any{
		"src": patchFile, "dest": dest, "strip": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged on a repeat application")
	}
}

func TestModulePatchRevert(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "greeting.txt")
	if err := os.WriteFile(dest, []byte("goodbye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patchFile := filepath.Join(dir, "greeting.patch")
	diff := "--- a/greeting.txt\n+++ b/greeting.txt\n@@ -1 +1 @@\n-hello\n+goodbye\n"
	if err := os.WriteFile(patchFile, []byte(diff), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := modulePatch(context.Background(), conn, map[string]any{
		"src": patchFile, "dest": dest, "strip": 1, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, got %+v", res)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("dest content = %q", data)
	}
}
