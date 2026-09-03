package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// stdinFor returns the stdin recorded for the first exact-match cmd in
// conn.Commands (conn.Stdins[i] holds "" for commands run without
// stdin, so picking by command name avoids relying on Stdins' final
// entry, which may belong to a later, stdin-less command like a
// reload).
func stdinFor(conn *fakeConn, cmd string) string {
	for i, c := range conn.Commands {
		if c == cmd {
			return conn.Stdins[i]
		}
	}
	return ""
}

func TestModuleSysctlPresentNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/sysctl.conf 2>/dev/null": {RC: 0, Stdout: "kernel.panic = 3\n"},
		"sysctl -p /etc/sysctl.conf":       {RC: 0},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "vm.swappiness", "value": "5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	want := "kernel.panic = 3\nvm.swappiness = 5\n"
	if got := stdinFor(conn, "cat > /etc/sysctl.conf"); got != want {
		t.Fatalf("written file = %q, want %q", got, want)
	}
}

func TestModuleSysctlPresentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/sysctl.conf 2>/dev/null": {RC: 0, Stdout: "vm.swappiness = 5\n"},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "vm.swappiness", "value": "5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSysctlUpdateExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/sysctl.conf 2>/dev/null": {RC: 0, Stdout: "vm.swappiness = 5\nkernel.panic = 3\n"},
		"sysctl -p /etc/sysctl.conf":       {RC: 0},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "vm.swappiness", "value": "10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	want := "vm.swappiness = 10\nkernel.panic = 3\n"
	if got := stdinFor(conn, "cat > /etc/sysctl.conf"); got != want {
		t.Fatalf("written file = %q, want %q", got, want)
	}
}

func TestModuleSysctlAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/sysctl.conf 2>/dev/null": {RC: 0, Stdout: "kernel.panic = 3\nvm.swappiness = 5\n"},
		"sysctl -p /etc/sysctl.conf":       {RC: 0},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "kernel.panic", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	want := "vm.swappiness = 5\n"
	if got := stdinFor(conn, "cat > /etc/sysctl.conf"); got != want {
		t.Fatalf("written file = %q, want %q", got, want)
	}
}

func TestModuleSysctlAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/sysctl.conf 2>/dev/null": {RC: 0, Stdout: "vm.swappiness = 5\n"},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "kernel.panic", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSysctlNoReload(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /tmp/test_sysctl.conf 2>/dev/null": {RC: 1},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "kernel.panic", "value": "3", "sysctl_file": "/tmp/test_sysctl.conf", "reload": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	for _, c := range conn.Commands {
		if c == "sysctl -p /tmp/test_sysctl.conf" {
			t.Fatal("reload=false must not run sysctl -p")
		}
	}
}

func TestModuleSysctlSet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/sysctl.conf 2>/dev/null": {RC: 0, Stdout: "net.ipv4.ip_forward = 1\n"},
		"sysctl -n net.ipv4.ip_forward":    {RC: 0, Stdout: "0"},
		"sysctl -w net.ipv4.ip_forward=1":  {RC: 0},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "net.ipv4.ip_forward", "value": "1", "sysctl_set": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: live value differs")
	}
}

func TestModuleSysctlSetAlreadyLive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/sysctl.conf 2>/dev/null": {RC: 0, Stdout: "net.ipv4.ip_forward = 1\n"},
		"sysctl -n net.ipv4.ip_forward":    {RC: 0, Stdout: "1"},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "net.ipv4.ip_forward", "value": "1", "sysctl_set": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSysctlIgnoreErrors(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/sysctl.conf 2>/dev/null": {RC: 1},
		"sysctl -e -p /etc/sysctl.conf":    {RC: 0},
	})
	res, err := moduleSysctl(context.Background(), conn, map[string]any{
		"name": "vm.swappiness", "value": "5", "ignoreerrors": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysctlValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSysctl(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleSysctl(context.Background(), conn, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error for missing value when state=present")
	}
	if _, err := moduleSysctl(context.Background(), conn, map[string]any{"name": "x", "value": "1", "state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}

func TestApplySysctlEntrySkipsCommentsAndBlanks(t *testing.T) {
	lines := []string{"", "# vm.swappiness = 99", "vm.swappiness = 5"}
	out, changed := applySysctlEntry(lines, "vm.swappiness", "10", "present")
	if !changed {
		t.Fatal("want changed")
	}
	if out[1] != "# vm.swappiness = 99" {
		t.Fatalf("comment line altered: %v", out)
	}
	if out[2] != "vm.swappiness = 10" {
		t.Fatalf("value not updated: %v", out)
	}
}
