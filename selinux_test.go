package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const testSelinuxConfig = "# comment\nSELINUX=disabled\nSELINUXTYPE=targeted\n"

func TestModuleSelinuxEnforceFromPermissive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/selinux/config": {RC: 0, Stdout: "SELINUX=permissive\nSELINUXTYPE=targeted\n"},
		"getenforce":              {RC: 0, Stdout: "Permissive"},
		"setenforce 1":            {RC: 0},
	})
	res, err := moduleSelinux(context.Background(), conn, map[string]any{
		"state": "enforcing", "policy": "targeted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["reboot_required"] != false {
		t.Fatalf("reboot_required = %v", res.Extra["reboot_required"])
	}
}

func TestModuleSelinuxAlreadyEnforcing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/selinux/config": {RC: 0, Stdout: "SELINUX=enforcing\nSELINUXTYPE=targeted\n"},
		"getenforce":              {RC: 0, Stdout: "Enforcing"},
	})
	res, err := moduleSelinux(context.Background(), conn, map[string]any{
		"state": "enforcing", "policy": "targeted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSelinuxDisableNeedsReboot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/selinux/config": {RC: 0, Stdout: "SELINUX=enforcing\nSELINUXTYPE=targeted\n"},
		"getenforce":              {RC: 0, Stdout: "Enforcing"},
	})
	res, err := moduleSelinux(context.Background(), conn, map[string]any{"state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed (config file rewritten)")
	}
	if res.Extra["reboot_required"] != true {
		t.Fatalf("reboot_required = %v", res.Extra["reboot_required"])
	}
}

func TestModuleSelinuxEnforceFromDisabledNeedsReboot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/selinux/config": {RC: 0, Stdout: testSelinuxConfig},
		"getenforce":              {RC: 0, Stdout: "Disabled"},
	})
	res, err := moduleSelinux(context.Background(), conn, map[string]any{
		"state": "enforcing", "policy": "targeted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["reboot_required"] != true {
		t.Fatalf("reboot_required = %v", res.Extra["reboot_required"])
	}
	found := false
	for _, c := range conn.Commands {
		if c == "setenforce 1" {
			found = true
		}
	}
	if found {
		t.Fatal("must not attempt setenforce from a disabled kernel state")
	}
}

func TestModuleSelinuxGetenforceMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/selinux/config": {RC: 0, Stdout: testSelinuxConfig},
		"getenforce":              {RC: 127},
	})
	res, err := moduleSelinux(context.Background(), conn, map[string]any{"state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: no getenforce on this host")
	}
}

func TestModuleSelinuxConfigMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/selinux/config": {RC: 1},
	})
	if _, err := moduleSelinux(context.Background(), conn, map[string]any{"state": "disabled"}); err == nil {
		t.Fatal("want error when configfile does not exist")
	}
}

func TestModuleSelinuxValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSelinux(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing state")
	}
	if _, err := moduleSelinux(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
	if _, err := moduleSelinux(context.Background(), conn, map[string]any{"state": "enforcing"}); err == nil {
		t.Fatal("want error: policy required unless disabled")
	}
}

func TestSetConfigKV(t *testing.T) {
	lines := []string{"# c", "SELINUX=disabled", "SELINUXTYPE=targeted"}
	out, changed := setConfigKV(lines, "SELINUX", "enforcing")
	if !changed || out[1] != "SELINUX=enforcing" {
		t.Fatalf("out = %v changed = %v", out, changed)
	}
	out2, changed2 := setConfigKV(out, "SELINUX", "enforcing")
	if changed2 {
		t.Fatalf("want no further change, got %v", out2)
	}
	out3, changed3 := setConfigKV([]string{"# c"}, "SELINUX", "enforcing")
	if !changed3 || out3[len(out3)-1] != "SELINUX=enforcing" {
		t.Fatalf("out3 = %v", out3)
	}
}
