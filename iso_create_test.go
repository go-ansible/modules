package modules

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIsoCreateCommand(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /root/testfile.yml": {RC: 0},
		"mkdir -p /tmp":              {RC: 0},
		"xorriso -as mkisofs -o /tmp/test.iso -iso-level 3 -graft-points testfile.yml=/root/testfile.yml": {RC: 0},
	})
	res, err := moduleIsoCreate(context.Background(), fc, map[string]any{
		"src_files": []any{"/root/testfile.yml"}, "dest_iso": "/tmp/test.iso", "interchange_level": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIsoCreateRockRidgeJolietVolIdent(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /root/testfile.yml": {RC: 0},
		"mkdir -p /tmp":              {RC: 0},
		"xorriso -as mkisofs -o /tmp/test.iso -iso-level 3 -V WIN_AUTOINSTALL -r -J -graft-points testfile.yml=/root/testfile.yml": {RC: 0},
	})
	res, err := moduleIsoCreate(context.Background(), fc, map[string]any{
		"src_files": []any{"/root/testfile.yml"}, "dest_iso": "/tmp/test.iso",
		"interchange_level": 3, "rock_ridge": "1.09", "joliet": 3, "vol_ident": "WIN_AUTOINSTALL",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIsoCreateMissingSrcFile(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /root/nope.yml": {RC: 1},
	})
	res, err := moduleIsoCreate(context.Background(), fc, map[string]any{
		"src_files": []any{"/root/nope.yml"}, "dest_iso": "/tmp/test.iso",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when a src_files entry does not exist")
	}
}

func TestModuleIsoCreateMissingArgs(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIsoCreate(context.Background(), fc, map[string]any{"dest_iso": "/tmp/x.iso"}); err == nil {
		t.Fatal("want error for missing/empty src_files")
	}
	if _, err := moduleIsoCreate(context.Background(), fc, map[string]any{"src_files": []any{"/a"}}); err == nil {
		t.Fatal("want error for missing dest_iso")
	}
}

func TestModuleIsoCreateBadInterchangeLevel(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIsoCreate(context.Background(), fc, map[string]any{
		"src_files": []any{"/a"}, "dest_iso": "/tmp/x.iso", "interchange_level": 9,
	}); err == nil {
		t.Fatal("want error for an out-of-range interchange_level")
	}
}

// TestModuleIsoCreateRealXorriso exercises the real xorriso binary end
// to end (create, then list contents) — skipped when xorriso is not on
// PATH, matching git_test.go's own "a real binary is a reasonable
// baseline dependency" precedent.
func TestModuleIsoCreateRealXorriso(t *testing.T) {
	if _, err := exec.LookPath("xorriso"); err != nil {
		t.Skip("xorriso not found in PATH")
	}
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "file1.txt")
	if err := os.WriteFile(srcFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.iso")

	conn := local()
	res, err := moduleIsoCreate(context.Background(), conn, map[string]any{
		"src_files": []any{srcFile}, "dest_iso": dest, "vol_ident": "MYVOL",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("iso was not created: %v", err)
	}

	out, err := exec.Command("xorriso", "-indev", dest, "-find", "/").CombinedOutput()
	if err != nil {
		t.Fatalf("xorriso -find failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "/FILE1.TXT") && !strings.Contains(string(out), "/file1.txt") {
		t.Fatalf("iso contents = %s, want file1.txt", out)
	}
}
