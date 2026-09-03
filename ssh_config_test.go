package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSSHConfigFindBlock(t *testing.T) {
	lines := []string{
		"# top comment",
		"Host alpha",
		"    HostName alpha.example.com",
		"    Port 22",
		"Host beta",
		"    HostName beta.example.com",
	}
	begin, end := sshConfigFindBlock(lines, "alpha")
	if begin != 1 || end != 3 {
		t.Fatalf("alpha block = [%d,%d], want [1,3]", begin, end)
	}
	begin, end = sshConfigFindBlock(lines, "beta")
	if begin != 4 || end != 5 {
		t.Fatalf("beta block = [%d,%d], want [4,5]", begin, end)
	}
	begin, end = sshConfigFindBlock(lines, "gamma")
	if begin != -1 || end != -1 {
		t.Fatalf("gamma block = [%d,%d], want [-1,-1]", begin, end)
	}
}

func TestSSHConfigPath(t *testing.T) {
	p, err := sshConfigPath(map[string]any{})
	if err != nil || p != "/etc/ssh/ssh_config" {
		t.Fatalf("default path = %q, %v", p, err)
	}
	p, err = sshConfigPath(map[string]any{"ssh_config_file": "/tmp/x/config"})
	if err != nil || p != "/tmp/x/config" {
		t.Fatalf("ssh_config_file path = %q, %v", p, err)
	}
	p, err = sshConfigPath(map[string]any{"user": "alice"})
	if err != nil || p != "~alice/.ssh/config" {
		t.Fatalf("user path = %q, %v", p, err)
	}
	if _, err := sshConfigPath(map[string]any{"user": "alice", "ssh_config_file": "/x"}); err == nil {
		t.Fatal("want error for mutually exclusive user + ssh_config_file")
	}
}

func TestModuleSSHConfigInsertNew(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config")
	conn := local()
	res, err := moduleSSHConfig(context.Background(), conn, map[string]any{
		"ssh_config_file": f,
		"host":            "example.com",
		"hostname":        "github.com",
		"identity_file":   "/home/user/.ssh/id_rsa",
		"port":            "2223",
		"other_options": map[string]any{
			"serveraliveinterval": "30",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "Host example.com\n    HostName github.com\n    Port 2223\n    IdentityFile /home/user/.ssh/id_rsa\n    serveraliveinterval 30\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleSSHConfigIdempotent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config")
	conn := local()
	args := map[string]any{
		"ssh_config_file": f,
		"host":            "example.com",
		"hostname":        "github.com",
	}
	if _, err := moduleSSHConfig(context.Background(), conn, args); err != nil {
		t.Fatal(err)
	}
	res, err := moduleSSHConfig(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged on second identical run")
	}
}

func TestModuleSSHConfigUpdateInPlace(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config")
	initial := "Host other\n    HostName other.example.com\nHost example.com\n    HostName old.example.com\n    Port 22\nHost zzz\n    HostName zzz.example.com\n"
	if err := os.WriteFile(f, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleSSHConfig(context.Background(), conn, map[string]any{
		"ssh_config_file": f,
		"host":            "example.com",
		"hostname":        "new.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "Host other\n    HostName other.example.com\nHost example.com\n    HostName new.example.com\nHost zzz\n    HostName zzz.example.com\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleSSHConfigAbsent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config")
	initial := "Host other\n    HostName other.example.com\nHost example.com\n    HostName old.example.com\n"
	if err := os.WriteFile(f, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleSSHConfig(context.Background(), conn, map[string]any{
		"ssh_config_file": f,
		"host":            "example.com",
		"state":           "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "Host other\n    HostName other.example.com\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleSSHConfigAbsentNotPresent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(f, []byte("Host other\n    HostName other.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleSSHConfig(context.Background(), conn, map[string]any{
		"ssh_config_file": f,
		"host":            "example.com",
		"state":           "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSSHConfigBoolOptions(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config")
	conn := local()
	res, err := moduleSSHConfig(context.Background(), conn, map[string]any{
		"ssh_config_file":   f,
		"host":              "example.com",
		"forward_agent":     true,
		"add_keys_to_agent": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, _ := os.ReadFile(f)
	want := "Host example.com\n    ForwardAgent yes\n    AddKeysToAgent no\n"
	if string(data) != want {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestModuleSSHConfigProxyMutuallyExclusive(t *testing.T) {
	conn := local()
	if _, err := moduleSSHConfig(context.Background(), conn, map[string]any{
		"ssh_config_file": filepath.Join(t.TempDir(), "config"),
		"host":            "example.com",
		"proxycommand":    "nc -w 60 %h %p",
		"proxyjump":       "bastion",
	}); err == nil {
		t.Fatal("want error for proxycommand + proxyjump")
	}
}

func TestModuleSSHConfigOtherOptionsValidation(t *testing.T) {
	conn := local()
	f := filepath.Join(t.TempDir(), "config")
	if _, err := moduleSSHConfig(context.Background(), conn, map[string]any{
		"ssh_config_file": f, "host": "example.com",
		"other_options": map[string]any{"UpperCase": "x"},
	}); err == nil {
		t.Fatal("want error for upper-case other_options key")
	}
	if _, err := moduleSSHConfig(context.Background(), conn, map[string]any{
		"ssh_config_file": f, "host": "example.com",
		"other_options": map[string]any{"foo": 5},
	}); err == nil {
		t.Fatal("want error for non-string other_options value")
	}
}

func TestModuleSSHConfigValidation(t *testing.T) {
	conn := local()
	if _, err := moduleSSHConfig(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing host")
	}
	if _, err := moduleSSHConfig(context.Background(), conn, map[string]any{"host": "h", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
