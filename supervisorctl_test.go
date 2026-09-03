package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestSupervisorctlBaseCmd(t *testing.T) {
	got := supervisorctlBaseCmd(map[string]any{
		"config": "/etc/supervisord.conf", "username": "test", "password": "testpass",
		"server_url": "http://localhost:9001", "supervisorctl_path": "/usr/local/bin/supervisorctl",
	})
	want := "/usr/local/bin/supervisorctl -c /etc/supervisord.conf -u test -p testpass -s http://localhost:9001"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSupervisorctlBaseCmdDefault(t *testing.T) {
	if got := supervisorctlBaseCmd(map[string]any{}); got != "supervisorctl" {
		t.Fatalf("got %q", got)
	}
}

func TestSupervisorctlStatusParsing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app":  {RC: 0, Stdout: "my_app                           RUNNING   pid 123, uptime 0:00:05"},
		"supervisorctl status stopped": {RC: 0, Stdout: "stopped                          STOPPED   Not started"},
		"supervisorctl status missing": {RC: 1, Stdout: "missing: ERROR (no such process)"},
	})
	running, exists, err := supervisorctlStatus(context.Background(), conn, "supervisorctl", "my_app")
	if err != nil || !running || !exists {
		t.Fatalf("running=%v exists=%v err=%v", running, exists, err)
	}
	running, exists, err = supervisorctlStatus(context.Background(), conn, "supervisorctl", "stopped")
	if err != nil || running || !exists {
		t.Fatalf("running=%v exists=%v err=%v", running, exists, err)
	}
	running, exists, err = supervisorctlStatus(context.Background(), conn, "supervisorctl", "missing")
	if err != nil || running || exists {
		t.Fatalf("running=%v exists=%v err=%v", running, exists, err)
	}
}

func TestModuleSupervisorctlPresentAddsWhenMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app": {RC: 1, Stdout: "my_app: ERROR (no such process)"},
	})
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	wantCmds := []string{"supervisorctl status my_app", "supervisorctl reread", "supervisorctl add my_app"}
	supervisorctlAssertCommands(t, conn.Commands, wantCmds)
}

func TestModuleSupervisorctlStartedAlreadyRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app": {RC: 0, Stdout: "my_app RUNNING pid 1, uptime 0:00:01"},
	})
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSupervisorctlStartedNeedsStart(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app": {RC: 0, Stdout: "my_app STOPPED Not started"},
	})
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "supervisorctl start my_app" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleSupervisorctlStoppedRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app": {RC: 0, Stdout: "my_app RUNNING pid 1, uptime 0:00:01"},
	})
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "stopped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSupervisorctlStoppedAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app": {RC: 0, Stdout: "my_app STOPPED Not started"},
	})
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "stopped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSupervisorctlRestartedAlwaysChanged(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "restarted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	supervisorctlAssertCommands(t, conn.Commands, []string{"supervisorctl update", "supervisorctl restart my_app"})
}

func TestModuleSupervisorctlAbsentNotExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app": {RC: 1, Stdout: "my_app: ERROR (no such process)"},
	})
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSupervisorctlAbsentRunningFailsWithoutStopBeforeRemoving(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app": {RC: 0, Stdout: "my_app RUNNING pid 1, uptime 0:00:01"},
	})
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: still running")
	}
}

func TestModuleSupervisorctlAbsentRunningStopsFirst(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"supervisorctl status my_app": {RC: 0, Stdout: "my_app RUNNING pid 1, uptime 0:00:01"},
	})
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "absent", "stop_before_removing": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	supervisorctlAssertCommands(t, conn.Commands, []string{
		"supervisorctl status my_app", "supervisorctl stop my_app", "supervisorctl reread", "supervisorctl remove my_app",
	})
}

func TestModuleSupervisorctlSignalled(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "signalled", "signal": "USR1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "supervisorctl signal USR1 my_app" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleSupervisorctlSignalledMissingSignal(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "signalled",
	}); err == nil {
		t.Fatal("want error: signal required for state=signalled")
	}
}

func TestModuleSupervisorctlMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSupervisorctl(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleSupervisorctl(context.Background(), conn, map[string]any{"name": "my_app"}); err == nil {
		t.Fatal("want error for missing state")
	}
}

func TestModuleSupervisorctlInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSupervisorctl(context.Background(), conn, map[string]any{
		"name": "my_app", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

// supervisorctlAssertCommands checks that every command in want was issued, in order,
// as a (not necessarily contiguous) subsequence of got.
func supervisorctlAssertCommands(t *testing.T, got, want []string) {
	t.Helper()
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Fatalf("commands = %v, want (in order) %v", got, want)
	}
}
