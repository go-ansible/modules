package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestKernelBlacklistApplyEntryAppend(t *testing.T) {
	out, changed := kernelBlacklistApplyEntry(nil, "nouveau", "present")
	if !changed || len(out) != 1 || out[0] != "blacklist nouveau" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestKernelBlacklistApplyEntryAlreadyPresent(t *testing.T) {
	existing := []string{"blacklist nouveau"}
	out, changed := kernelBlacklistApplyEntry(existing, "nouveau", "present")
	if changed {
		t.Fatal("want unchanged")
	}
	if len(out) != 1 {
		t.Fatalf("out=%v", out)
	}
}

func TestKernelBlacklistApplyEntryCommentedLineNotAMatch(t *testing.T) {
	existing := []string{"# blacklist nouveau"}
	out, changed := kernelBlacklistApplyEntry(existing, "nouveau", "present")
	if !changed || len(out) != 2 {
		t.Fatalf("want a fresh active line appended: out=%v changed=%v", out, changed)
	}
}

func TestKernelBlacklistApplyEntryAbsent(t *testing.T) {
	existing := []string{"blacklist nouveau", "blacklist other"}
	out, changed := kernelBlacklistApplyEntry(existing, "nouveau", "absent")
	if !changed || len(out) != 1 || out[0] != "blacklist other" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestKernelBlacklistApplyEntryAbsentNotFound(t *testing.T) {
	existing := []string{"blacklist other"}
	out, changed := kernelBlacklistApplyEntry(existing, "nouveau", "absent")
	if changed || len(out) != 1 {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestModuleKernelBlacklistPresentCreatesFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "blacklist-ansible.conf")
	conn := local()
	res, err := moduleKernelBlacklist(context.Background(), conn, map[string]any{
		"name": "nouveau", "blacklist_file": f,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "blacklist nouveau\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleKernelBlacklistPresentIdempotent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "blacklist-ansible.conf")
	if err := os.WriteFile(f, []byte("blacklist nouveau\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleKernelBlacklist(context.Background(), conn, map[string]any{
		"name": "nouveau", "blacklist_file": f,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKernelBlacklistAbsent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "blacklist-ansible.conf")
	if err := os.WriteFile(f, []byte("blacklist nouveau\nblacklist other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleKernelBlacklist(context.Background(), conn, map[string]any{
		"name": "nouveau", "blacklist_file": f, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "blacklist other\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleKernelBlacklistMissingName(t *testing.T) {
	conn := local()
	if _, err := moduleKernelBlacklist(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleKernelBlacklistInvalidState(t *testing.T) {
	conn := local()
	if _, err := moduleKernelBlacklist(context.Background(), conn, map[string]any{
		"name": "nouveau", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
