package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPamdTokenize(t *testing.T) {
	toks := pamdTokenize("session\t[success=1 default=ignore]  pam_succeed_if.so crond quiet")
	want := []string{"session", "[success=1 default=ignore]", "pam_succeed_if.so", "crond", "quiet"}
	if len(toks) != len(want) {
		t.Fatalf("toks=%v", toks)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Fatalf("toks=%v want=%v", toks, want)
		}
	}
}

func TestPamdArgsPresentReplacesKeyedValue(t *testing.T) {
	out := pamdArgsPresent([]string{"preauth", "deny=3"}, []string{"deny=5"})
	want := []string{"preauth", "deny=5"}
	if len(out) != len(want) || out[0] != want[0] || out[1] != want[1] {
		t.Fatalf("out=%v", out)
	}
}

func TestPamdArgsPresentAppendsNew(t *testing.T) {
	out := pamdArgsPresent([]string{"crond"}, []string{"quiet"})
	if len(out) != 2 || out[1] != "quiet" {
		t.Fatalf("out=%v", out)
	}
}

func TestPamdArgsAbsent(t *testing.T) {
	out := pamdArgsAbsent([]string{"crond", "quiet"}, []string{"quiet"})
	if len(out) != 1 || out[0] != "crond" {
		t.Fatalf("out=%v", out)
	}
}

func pamdWriteService(t *testing.T, content string) (dir, name string) {
	t.Helper()
	dir = t.TempDir()
	name = "system-auth"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, name
}

func TestModulePamdUpdatedControl(t *testing.T) {
	dir, name := pamdWriteService(t, "auth required pam_faillock.so\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "auth", "control": "required", "module_path": "pam_faillock.so",
		"new_control": "sufficient",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, name))
	if string(data) != "auth sufficient pam_faillock.so\n" {
		t.Fatalf("content = %q", data)
	}
	if res.Extra["change_count"] != 1 {
		t.Fatalf("change_count = %v", res.Extra["change_count"])
	}
}

func TestModulePamdUpdatedNoMatchFails(t *testing.T) {
	dir, name := pamdWriteService(t, "auth required pam_other.so\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "auth", "control": "required", "module_path": "pam_faillock.so",
		"new_control": "sufficient",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, got %+v", res)
	}
}

func TestModulePamdInsertAfter(t *testing.T) {
	dir, name := pamdWriteService(t, "auth sufficient pam_rootok.so\nauth required pam_deny.so\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "auth", "control": "sufficient", "module_path": "pam_rootok.so",
		"new_type": "auth", "new_control": "required", "new_module_path": "pam_wheel.so",
		"module_arguments": "use_uid", "state": "after",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, name))
	want := "auth sufficient pam_rootok.so\nauth required pam_wheel.so use_uid\nauth required pam_deny.so\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModulePamdInsertBefore(t *testing.T) {
	dir, name := pamdWriteService(t, "auth required pam_faillock.so\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "auth", "control": "required", "module_path": "pam_faillock.so",
		"new_type": "auth", "new_control": "sufficient", "new_module_path": "pam_permit.so",
		"state": "before",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, name))
	want := "auth sufficient pam_permit.so\nauth required pam_faillock.so\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModulePamdInsertMissingNewFields(t *testing.T) {
	dir, name := pamdWriteService(t, "auth required pam_faillock.so\n")
	conn := local()
	if _, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "auth", "control": "required", "module_path": "pam_faillock.so",
		"state": "before",
	}); err == nil {
		t.Fatal("want error: new_type/new_control/new_module_path required for before/after")
	}
}

func TestModulePamdAbsent(t *testing.T) {
	dir, name := pamdWriteService(t, "auth required pam_faillock.so\nauth required pam_deny.so\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "auth", "control": "required", "module_path": "pam_faillock.so",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, name))
	if string(data) != "auth required pam_deny.so\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModulePamdAbsentNoMatchIsNoop(t *testing.T) {
	dir, name := pamdWriteService(t, "auth required pam_deny.so\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "auth", "control": "required", "module_path": "pam_faillock.so",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, want a no-op ok", res)
	}
}

func TestModulePamdArgsPresent(t *testing.T) {
	dir, name := pamdWriteService(t, "session [success=1 default=ignore] pam_succeed_if.so crond\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "session", "control": "[success=1 default=ignore]", "module_path": "pam_succeed_if.so",
		"module_arguments": []string{"crond", "quiet"}, "state": "args_present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, name))
	want := "session [success=1 default=ignore] pam_succeed_if.so crond quiet\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModulePamdArgsAbsent(t *testing.T) {
	dir, name := pamdWriteService(t, "session [success=1 default=ignore] pam_succeed_if.so crond quiet\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "session", "control": "[success=1 default=ignore]", "module_path": "pam_succeed_if.so",
		"module_arguments": "quiet", "state": "args_absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, name))
	want := "session [success=1 default=ignore] pam_succeed_if.so crond\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModulePamdBackup(t *testing.T) {
	dir, name := pamdWriteService(t, "auth required pam_faillock.so\n")
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": name, "path": dir,
		"type": "auth", "control": "required", "module_path": "pam_faillock.so",
		"new_control": "sufficient", "backup": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["backupdest"] == nil {
		t.Fatal("want backupdest in Extra")
	}
}

func TestModulePamdMissingArgs(t *testing.T) {
	conn := local()
	if _, err := modulePamd(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModulePamdServiceFileMissing(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := modulePamd(context.Background(), conn, map[string]any{
		"name": "nope", "path": dir,
		"type": "auth", "control": "required", "module_path": "pam_faillock.so",
		"new_control": "sufficient",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: service file does not exist")
	}
}
