package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestRegistryDefault(t *testing.T) {
	r := Default()
	want := []string{
		"acl", "apt", "apt_key", "apt_repository", "assemble", "assert",
		"async_status", "at", "authorized_key", "blockinfile", "command",
		"copy", "cron", "deb822_repository", "debconf", "debug", "dnf",
		"dnf5", "dpkg_selections", "expect", "fail", "fetch", "file",
		"find", "firewalld", "firewalld_info", "gather_facts", "get_url",
		"getent", "git", "group", "hostname", "iptables", "known_hosts",
		"lineinfile", "mount", "mount_facts", "package", "package_facts",
		"patch", "pause", "ping", "pip", "raw", "reboot", "replace",
		"rhel_facts", "rhel_rpm_ostree", "rpm_key", "rpm_ostree_upgrade",
		"script", "seboolean", "selinux", "service", "service_facts",
		"set_fact", "set_stats", "setup", "shell", "slurp", "stat",
		"subversion", "synchronize", "sysctl", "systemd",
		"systemd_service", "sysvinit", "tempfile", "template",
		"unarchive", "uri", "user", "validate_argument_spec", "wait_for",
		"wait_for_connection", "yum_repository",
	}
	names := r.Names()
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %d entries", names, len(want))
	}
	for _, w := range want {
		if _, ok := r.Get(w); !ok {
			t.Errorf("module %q not registered", w)
		}
	}
}

func TestRegistryRunUnknownModule(t *testing.T) {
	r := Default()
	res, err := r.Run(context.Background(), "no_such_module", local(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an unknown module name")
	}
}

func TestRegistryRunKnownModule(t *testing.T) {
	r := Default()
	res, err := r.Run(context.Background(), "debug", local(), map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "hi" {
		t.Fatalf("Msg = %q", res.Msg)
	}
}

func TestRegistryOverride(t *testing.T) {
	r := NewRegistry()
	r.Register("x", func(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
		return Ok("first"), nil
	})
	r.Register("x", func(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
		return Ok("second"), nil
	})
	res, err := r.Run(context.Background(), "x", local(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "second" {
		t.Fatalf("Msg = %q, want the later Register to win", res.Msg)
	}
}

func TestResultWithExtra(t *testing.T) {
	r := Ok("m").WithExtra("a", 1).WithExtra("b", 2)
	if r.Extra["a"] != 1 || r.Extra["b"] != 2 {
		t.Fatalf("Extra = %v", r.Extra)
	}
}

func TestFailHelper(t *testing.T) {
	r := Fail("boom")
	if !r.Failed || r.Msg != "boom" {
		t.Fatalf("r = %+v", r)
	}
}

func TestChangedHelper(t *testing.T) {
	r := Changed("did it")
	if !r.Changed || r.Msg != "did it" {
		t.Fatalf("r = %+v", r)
	}
}
