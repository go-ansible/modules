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

func TestModuleIsoCustomizeCommand(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /path/to/ubuntu.iso":  {RC: 0},
		"test -e /path/to":             {RC: 0},
		"test -e /path/to/grub.cfg":    {RC: 0},
		"test -e /path/to/ubuntu.seed": {RC: 0},
		"xorriso -indev /path/to/ubuntu.iso -outdev /path/to/customized.iso -rm_r /boot.catalog -- -map /path/to/grub.cfg /boot/grub/grub.cfg -map /path/to/ubuntu.seed /preseed/ubuntu.seed -commit": {RC: 0},
	})
	res, err := moduleIsoCustomize(context.Background(), fc, map[string]any{
		"src_iso":      "/path/to/ubuntu.iso",
		"dest_iso":     "/path/to/customized.iso",
		"delete_files": []any{"/boot.catalog"},
		"add_files": []any{
			map[string]any{"src_file": "/path/to/grub.cfg", "dest_file": "/boot/grub/grub.cfg"},
			map[string]any{"src_file": "/path/to/ubuntu.seed", "dest_file": "/preseed/ubuntu.seed"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	addFiles, ok := res.Extra["add_files"].([]map[string]any)
	if !ok || len(addFiles) != 2 {
		t.Fatalf("add_files = %#v", res.Extra["add_files"])
	}
}

func TestModuleIsoCustomizeAddOnlyNormalizesLeadingSlash(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /path/to/ubuntu.iso": {RC: 0},
		"test -e /path/to":            {RC: 0},
		"test -e /path/to/grub.cfg":   {RC: 0},
		"xorriso -indev /path/to/ubuntu.iso -outdev /path/to/customized.iso -map /path/to/grub.cfg /boot/grub/grub.cfg -commit": {RC: 0},
	})
	res, err := moduleIsoCustomize(context.Background(), fc, map[string]any{
		"src_iso":  "/path/to/ubuntu.iso",
		"dest_iso": "/path/to/customized.iso",
		"add_files": []any{
			map[string]any{"src_file": "/path/to/grub.cfg", "dest_file": "boot/grub/grub.cfg"}, // no leading /
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIsoCustomizeMissingSrcISO(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /path/to/ubuntu.iso": {RC: 1},
	})
	res, err := moduleIsoCustomize(context.Background(), fc, map[string]any{
		"src_iso": "/path/to/ubuntu.iso", "dest_iso": "/path/to/customized.iso",
		"delete_files": []any{"/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when src_iso does not exist")
	}
}

func TestModuleIsoCustomizeRequiresOneOf(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIsoCustomize(context.Background(), fc, map[string]any{
		"src_iso": "/x.iso", "dest_iso": "/y.iso",
	}); err == nil {
		t.Fatal("want error when neither delete_files nor add_files is given")
	}
}

func TestModuleIsoCustomizeMissingArgs(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIsoCustomize(context.Background(), fc, map[string]any{"dest_iso": "/y.iso"}); err == nil {
		t.Fatal("want error for missing src_iso")
	}
}

// TestModuleIsoCustomizeRealXorriso exercises the real xorriso binary
// end to end: build a source ISO with iso_create, customize it (add +
// delete), then verify the result with `xorriso -find`.
func TestModuleIsoCustomizeRealXorriso(t *testing.T) {
	if _, err := exec.LookPath("xorriso"); err != nil {
		t.Skip("xorriso not found in PATH")
	}
	dir := t.TempDir()
	origFile := filepath.Join(dir, "orig.txt")
	if err := os.WriteFile(origFile, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(newFile, []byte("new-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcISO := filepath.Join(dir, "src.iso")
	destISO := filepath.Join(dir, "dest.iso")

	conn := local()
	if _, err := moduleIsoCreate(context.Background(), conn, map[string]any{
		"src_files": []any{origFile}, "dest_iso": srcISO,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := moduleIsoCustomize(context.Background(), conn, map[string]any{
		"src_iso": srcISO, "dest_iso": destISO,
		"delete_files": []any{"/" + filepath.Base(origFile)},
		"add_files": []any{
			map[string]any{"src_file": newFile, "dest_file": "/subdir/new.txt"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}

	out, err := exec.Command("xorriso", "-indev", destISO, "-find", "/").CombinedOutput()
	if err != nil {
		t.Fatalf("xorriso -find failed: %v\n%s", err, out)
	}
	if strings.Contains(strings.ToUpper(string(out)), strings.ToUpper(filepath.Base(origFile))) {
		t.Fatalf("deleted file still present: %s", out)
	}
	if !strings.Contains(string(out), "/subdir") {
		t.Fatalf("added file missing: %s", out)
	}
}
