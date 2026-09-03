package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSyspatchApplyNoneAvailable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"syspatch -c": {RC: 0, Stdout: ""},
	})
	res, err := moduleSyspatch(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if res.Extra["reboot_needed"] != false {
		t.Fatalf("reboot_needed = %v", res.Extra["reboot_needed"])
	}
}

func TestModuleSyspatchApplyAvailable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"syspatch -c": {RC: 0, Stdout: "007_ntpd\n"},
		"syspatch":    {RC: 0, Stdout: "Installing patch 007_ntpd\n"},
	})
	res, err := moduleSyspatch(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["reboot_needed"] != false {
		t.Fatalf("reboot_needed = %v", res.Extra["reboot_needed"])
	}
}

func TestModuleSyspatchApplyRebootNeeded(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"syspatch -c": {RC: 0, Stdout: "008_kernel\n"},
		"syspatch":    {RC: 0, Stdout: "Installing patch 008_kernel\nReboot required\n"},
	})
	res, err := moduleSyspatch(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["reboot_needed"] != true {
		t.Fatalf("reboot_needed = %v", res.Extra["reboot_needed"])
	}
}

func TestModuleSyspatchRevertOne(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"syspatch -l": {RC: 0, Stdout: "007_ntpd\n"},
		"syspatch -r": {RC: 0},
	})
	res, err := moduleSyspatch(context.Background(), conn, map[string]any{"revert": "one"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSyspatchRevertAllNoneApplied(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"syspatch -l": {RC: 0, Stdout: ""},
	})
	res, err := moduleSyspatch(context.Background(), conn, map[string]any{"revert": "all"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSyspatchInvalidRevert(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSyspatch(context.Background(), conn, map[string]any{"revert": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}
