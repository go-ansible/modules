package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestSudoersBuildContentUserRule(t *testing.T) {
	content, err := sudoersBuildContent(map[string]any{
		"host": "webserver", "commands": []string{"/usr/local/bin/gather-app-metrics"},
	}, "", "monitoring")
	if err != nil {
		t.Fatal(err)
	}
	want := "%monitoring webserver = NOPASSWD: /usr/local/bin/gather-app-metrics\n"
	if content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestSudoersBuildContentRunasNoPasswordFalseSetenv(t *testing.T) {
	content, err := sudoersBuildContent(map[string]any{
		"commands": []string{"ALL"}, "runas": "alice", "nopassword": false, "setenv": true,
	}, "bob", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "bob ALL = (alice) SETENV: ALL\n"
	if content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestSudoersBuildContentDefaultsAndCommands(t *testing.T) {
	content, err := sudoersBuildContent(map[string]any{
		"commands": []string{"ALL"}, "nopassword": false, "defaults": []string{"!targetpw"},
	}, "", "operators")
	if err != nil {
		t.Fatal(err)
	}
	want := "Defaults:%operators !targetpw\n%operators ALL = ALL\n"
	if content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestSudoersBuildContentDefaultsOnlyNoCommands(t *testing.T) {
	content, err := sudoersBuildContent(map[string]any{
		"defaults": []string{"!targetpw"},
	}, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "Defaults:alice !targetpw\n"
	if content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestSudoersBuildContentNeitherCommandsNorDefaults(t *testing.T) {
	if _, err := sudoersBuildContent(map[string]any{}, "alice", ""); err == nil {
		t.Fatal("want error: at least one of commands or defaults required")
	}
}

func TestModuleSudoersUserGroupMutuallyExclusive(t *testing.T) {
	conn := local()
	if _, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "x", "user": "alice", "group": "wheel", "commands": []string{"ALL"},
	}); err == nil {
		t.Fatal("want error: user and group mutually exclusive")
	}
}

func TestModuleSudoersNeitherUserNorGroup(t *testing.T) {
	conn := local()
	if _, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "x", "commands": []string{"ALL"},
	}); err == nil {
		t.Fatal("want error: exactly one of user or group required")
	}
}

func TestModuleSudoersCreatesFileNoVisudo(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "allow-backup", "user": "backup", "commands": []string{"/usr/local/bin/backup"},
		"sudoers_path": dir, "validation": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	dest := filepath.Join(dir, "allow-backup")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	want := "backup ALL = NOPASSWD: /usr/local/bin/backup\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o440 {
		t.Fatalf("mode = %v, want 0440", info.Mode().Perm())
	}
}

func TestModuleSudoersIdempotent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "allow-backup")
	content := "backup ALL = NOPASSWD: /usr/local/bin/backup\n"
	if err := os.WriteFile(dest, []byte(content), 0o440); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "allow-backup", "user": "backup", "commands": []string{"/usr/local/bin/backup"},
		"sudoers_path": dir, "validation": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSudoersAbsent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "allow-backup")
	if err := os.WriteFile(dest, []byte("x\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "allow-backup", "sudoers_path": dir, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("want file removed")
	}
}

func TestModuleSudoersAbsentAlready(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "allow-backup", "sudoers_path": dir, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSudoersValidationRequiredNoVisudo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/sudoers.d/allow-backup": {RC: 1},
		"command -v visudo >/dev/null 2>&1":   {RC: 1},
	})
	res, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "allow-backup", "user": "backup", "commands": []string{"/usr/local/bin/backup"},
		"validation": "required",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed: visudo required but not found, got %+v", res)
	}
}

func TestModuleSudoersValidationDetectVisudoFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/sudoers.d/allow-backup":    {RC: 1},
		"command -v visudo >/dev/null 2>&1":      {RC: 0},
		"visudo -c -f /tmp/sudoers-allow-backup": {RC: 1, Stderr: "syntax error near line 1"},
	})
	res, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "allow-backup", "user": "backup", "commands": []string{"/usr/local/bin/backup"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed: visudo validation failed, got %+v", res)
	}
	if res.Msg == "" {
		t.Fatal("want visudo's error text in Msg")
	}
}

func TestModuleSudoersValidationDetectVisudoPasses(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/sudoers.d/allow-backup":    {RC: 1},
		"command -v visudo >/dev/null 2>&1":      {RC: 0},
		"visudo -c -f /tmp/sudoers-allow-backup": {RC: 0},
	})
	res, err := moduleSudoers(context.Background(), conn, map[string]any{
		"name": "allow-backup", "user": "backup", "commands": []string{"/usr/local/bin/backup"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, got %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "mv /tmp/sudoers-allow-backup /etc/sudoers.d/allow-backup" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want mv into place, commands = %v", conn.Commands)
	}
}
