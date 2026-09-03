package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleCrypttabMissingName(t *testing.T) {
	conn := local()
	if _, err := moduleCrypttab(context.Background(), conn, map[string]any{"state": "present"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleCrypttabMissingState(t *testing.T) {
	conn := local()
	if _, err := moduleCrypttab(context.Background(), conn, map[string]any{"name": "crypt1"}); err == nil {
		t.Fatal("want error for missing state")
	}
}

func TestModuleCrypttabInvalidState(t *testing.T) {
	conn := local()
	if _, err := moduleCrypttab(context.Background(), conn, map[string]any{"name": "crypt1", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleCrypttabPresentRequiresBackingDeviceForNewEntry(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	conn := local()
	if _, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "present", "path": f,
	}); err == nil {
		t.Fatal("want error: no existing entry and no backing_device given")
	}
}

func TestModuleCrypttabCreateNew(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "present", "path": f, "backing_device": "/dev/sdb1", "password": "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "crypt1 /dev/sdb1 none none\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleCrypttabStripsDevMapperPrefix(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	conn := local()
	if _, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "/dev/mapper/crypt1", "state": "present", "path": f, "backing_device": "/dev/sdb1",
	}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(f)
	if !strings.HasPrefix(string(data), "crypt1 ") {
		t.Fatalf("content = %q, want it to start with the bare name", data)
	}
}

func TestModuleCrypttabAlreadyPresentUnchanged(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	if err := os.WriteFile(f, []byte("crypt1 /dev/sdb1 none none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "present", "path": f, "backing_device": "/dev/sdb1", "password": "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleCrypttabUpdateDevice(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	if err := os.WriteFile(f, []byte("crypt1 /dev/sdb1 none none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "present", "path": f, "backing_device": "/dev/sdc1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "crypt1 /dev/sdc1 none none\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleCrypttabAbsentRemoves(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	if err := os.WriteFile(f, []byte("crypt1 /dev/sdb1 none none\ncrypt2 /dev/sdc1 none none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{"name": "crypt1", "state": "absent", "path": f})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "crypt2 /dev/sdc1 none none\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleCrypttabAbsentAlreadyGone(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{"name": "crypt1", "state": "absent", "path": f})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleCrypttabOptsPresentOnMissingEntryFails(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "opts_present", "path": f, "opts": "discard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: no existing entry to add options to")
	}
}

func TestModuleCrypttabOptsPresentAdds(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	if err := os.WriteFile(f, []byte("crypt1 /dev/sdb1 none luks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "opts_present", "path": f, "opts": "discard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "crypt1 /dev/sdb1 none luks,discard\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleCrypttabOptsPresentUpdatesValue(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	if err := os.WriteFile(f, []byte("crypt1 /dev/sdb1 none cipher=aes-cbc-essiv:sha256\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "opts_present", "path": f, "opts": "cipher=aes-xts-plain64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "crypt1 /dev/sdb1 none cipher=aes-xts-plain64\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleCrypttabOptsPresentAlreadySetUnchanged(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	if err := os.WriteFile(f, []byte("crypt1 /dev/sdb1 none luks,discard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "opts_present", "path": f, "opts": "discard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleCrypttabOptsAbsentRemoves(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	if err := os.WriteFile(f, []byte("crypt1 /dev/sdb1 none luks,discard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "opts_absent", "path": f, "opts": "discard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "crypt1 /dev/sdb1 none luks\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleCrypttabOptsAbsentToEmptyBecomesNone(t *testing.T) {
	f := filepath.Join(t.TempDir(), "crypttab")
	if err := os.WriteFile(f, []byte("crypt1 /dev/sdb1 none luks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCrypttab(context.Background(), conn, map[string]any{
		"name": "crypt1", "state": "opts_absent", "path": f, "opts": "luks",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "crypt1 /dev/sdb1 none none\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestCrypttabEntryIndex(t *testing.T) {
	lines := []string{"# comment", "", "crypt1 /dev/sdb1 none none", "crypt2 /dev/sdc1 none none"}
	if crypttabEntryIndex(lines, "crypt1") != 2 {
		t.Fatalf("index = %d, want 2", crypttabEntryIndex(lines, "crypt1"))
	}
	if crypttabEntryIndex(lines, "nope") != -1 {
		t.Fatal("want -1 for a name not present")
	}
}

func TestCrypttabSplitOpts(t *testing.T) {
	for _, none := range []string{"", "none", "-"} {
		if got := crypttabSplitOpts(none); got != nil {
			t.Fatalf("crypttabSplitOpts(%q) = %v, want nil", none, got)
		}
	}
	if got := crypttabSplitOpts("luks,discard"); len(got) != 2 || got[0] != "luks" || got[1] != "discard" {
		t.Fatalf("got %v", got)
	}
}

func TestCrypttabMergeAndRemoveOpts(t *testing.T) {
	merged := crypttabMergeOpts([]string{"luks"}, []string{"discard"})
	if len(merged) != 2 || merged[0] != "luks" || merged[1] != "discard" {
		t.Fatalf("merged = %v", merged)
	}
	updated := crypttabMergeOpts([]string{"cipher=old"}, []string{"cipher=new"})
	if len(updated) != 1 || updated[0] != "cipher=new" {
		t.Fatalf("updated = %v", updated)
	}
	removed := crypttabRemoveOpts([]string{"luks", "discard"}, []string{"discard"})
	if len(removed) != 1 || removed[0] != "luks" {
		t.Fatalf("removed = %v", removed)
	}
}
