package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// This module had no test file at all, which is how #107 and #108 lasted:
// `daemon_reload` was accepted and silently did nothing, and `name` was
// demanded where real does not. Both are argument-handling defects, and
// argument handling is exactly what a scripted connection can pin.

func TestSystemdDaemonReloadAloneNeedsNoName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl daemon-reload": {RC: 0},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"daemon_reload": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v; real's argument_spec does not require name", res)
	}
	// Real sets changed only inside its `if unit:` block, so a
	// manager-only task reports ok.
	if res.Changed {
		t.Error("changed = true; real leaves it false for daemon_reload alone")
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "systemctl daemon-reload" {
		t.Fatalf("commands = %q, want exactly the reload", conn.Commands)
	}
}

// The alias real declares, which a playbook written against the docs may use.
func TestSystemdDaemonReloadDashAliasWorks(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"systemctl daemon-reload": {RC: 0}})
	if _, err := moduleSystemd(context.Background(), conn, map[string]any{"daemon-reload": true}); err != nil {
		t.Fatal(err)
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "systemctl daemon-reload" {
		t.Errorf("commands = %q, want the reload via the dashed alias", conn.Commands)
	}
}

// Order is the half of #107 that a "does it run at all" test would miss:
// real runs daemon-reload BEFORE touching the unit, so a unit file dropped
// in by an earlier task is visible to the start that follows.
func TestSystemdDaemonReloadRunsBeforeTheUnitWork(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl daemon-reload":  {RC: 0},
		"systemctl is-active web":  {RC: 3}, // inactive
		"systemctl start web":      {RC: 0},
		"systemctl daemon-reexec":  {RC: 0},
		"systemctl is-enabled web": {RC: 1},
		"systemctl enable web":     {RC: 0},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{
		"name": "web", "state": "started", "daemon_reload": true, "daemon_reexec": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("changed = false; starting an inactive unit is a change")
	}
	want := []string{
		"systemctl daemon-reload",
		"systemctl daemon-reexec",
		"systemctl is-active web",
		"systemctl start web",
	}
	if len(conn.Commands) != len(want) {
		t.Fatalf("commands = %q, want %q", conn.Commands, want)
	}
	for i := range want {
		if conn.Commands[i] != want[i] {
			t.Errorf("command %d = %q, want %q", i, conn.Commands[i], want[i])
		}
	}
}

func TestSystemdWithNoActionAtAllIsRefused(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"name": "web"})
	if err != nil {
		t.Fatal(err)
	}
	want := "one of the following is required: state, enabled, masked, daemon_reload, daemon_reexec"
	if !res.Failed || res.Msg != want {
		t.Errorf("msg = %q, want real's required_one_of wording %q", res.Msg, want)
	}
	if len(conn.Commands) != 0 {
		t.Errorf("ran %q before refusing", conn.Commands)
	}
}

// required_by, in real's own wording. `name` is required BY state/enabled/
// masked, not by the module.
func TestSystemdStateWithoutNameCarriesRealsSentence(t *testing.T) {
	for _, k := range []string{"state", "enabled"} {
		args := map[string]any{k: "started"}
		if k == "enabled" {
			args[k] = true
		}
		conn := newFakeConn(nil)
		res, err := moduleSystemd(context.Background(), conn, args)
		if err != nil {
			t.Fatal(err)
		}
		want := "missing parameter(s) required by '" + k + "': name"
		if !res.Failed || res.Msg != want {
			t.Errorf("for %s: msg = %q, want %q", k, res.Msg, want)
		}
	}
}

func TestSystemdDaemonReloadFailureCarriesRealsSentence(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl daemon-reload": {RC: 1, Stderr: "Failed to reload daemon: Access denied\n"},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"daemon_reload": true})
	if err != nil {
		t.Fatal(err)
	}
	want := "failure 1 during daemon-reload: Failed to reload daemon: Access denied"
	if !res.Failed || res.Msg != want {
		t.Errorf("msg = %q, want real's wording %q", res.Msg, want)
	}
}

// Real downgrades the failure to a warning when systemd is offline on the
// target. The variable is read ON THE TARGET, because that is the host whose
// systemd is offline — reading the control node's own environment would
// answer about the wrong machine.
func TestSystemdDaemonReloadToleratesSystemdOfflineOnTheTarget(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl daemon-reload":  {RC: 1, Stderr: "offline\n"},
		"printenv SYSTEMD_OFFLINE": {RC: 0, Stdout: "1\n"},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"daemon_reload": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v; real warns and continues when SYSTEMD_OFFLINE=1", res)
	}
	if !containsCmd(conn.Commands, "printenv SYSTEMD_OFFLINE") {
		t.Error("the variable was never read on the target")
	}

	// Control: without the variable the same failure fails the task, so the
	// assertion above is about the variable and not about a module that
	// never fails.
	strict := newFakeConn(map[string]remoteexec.Result{
		"systemctl daemon-reload": {RC: 1, Stderr: "offline\n"},
	})
	res2, err := moduleSystemd(context.Background(), strict, map[string]any{"daemon_reload": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Failed {
		t.Error("a failed daemon-reload passed with no SYSTEMD_OFFLINE set")
	}
}

// scope is the reason daemon_reload could not be added without it: reloading
// the system manager for a task that said `scope: user` is the wrong manager,
// and nothing would say so.
func TestSystemdScopeReachesTheNamedManager(t *testing.T) {
	for _, tc := range []struct{ scope, want string }{
		{"user", "systemctl --user daemon-reload"},
		{"global", "systemctl --global daemon-reload"},
		{"system", "systemctl daemon-reload"},
		{"", "systemctl daemon-reload"},
	} {
		conn := newFakeConn(map[string]remoteexec.Result{tc.want: {RC: 0}})
		args := map[string]any{"daemon_reload": true}
		if tc.scope != "" {
			args["scope"] = tc.scope
		}
		if _, err := moduleSystemd(context.Background(), conn, args); err != nil {
			t.Fatal(err)
		}
		if len(conn.Commands) != 1 || conn.Commands[0] != tc.want {
			t.Errorf("scope %q: commands = %q, want %q", tc.scope, conn.Commands, tc.want)
		}
	}
}

func TestSystemdRejectsAnUnknownScope(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSystemd(context.Background(), conn, map[string]any{
		"daemon_reload": true, "scope": "sideways",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "system, user, global") {
		t.Errorf("res = %+v, want the choices named", res)
	}
	if len(conn.Commands) != 0 {
		t.Errorf("ran %q before refusing", conn.Commands)
	}
}

func TestSystemdRefusesAGlobInTheName(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSystemd(context.Background(), conn, map[string]any{
		"name": "web*", "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "This module does not currently support using glob patterns, found '*' in service name: web*"
	if !res.Failed || res.Msg != want {
		t.Errorf("msg = %q, want real's wording %q", res.Msg, want)
	}
}

// masked is in real's required_one_of, so accepting it into that check and
// then ignoring it would be the same defect as #107. It is refused by name.
func TestSystemdMaskedIsRefusedNotIgnored(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSystemd(context.Background(), conn, map[string]any{
		"name": "web", "masked": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "masked is not implemented") {
		t.Errorf("res = %+v, want a loud refusal", res)
	}
	if len(conn.Commands) != 0 {
		t.Errorf("ran %q for an unimplemented argument", conn.Commands)
	}
}

func TestSystemdNameAliases(t *testing.T) {
	for _, key := range []string{"name", "service", "unit"} {
		conn := newFakeConn(map[string]remoteexec.Result{
			"systemctl is-active web": {RC: 0},
		})
		if _, err := moduleSystemd(context.Background(), conn, map[string]any{
			key: "web", "state": "started",
		}); err != nil {
			t.Fatal(err)
		}
		if !containsCmd(conn.Commands, "systemctl is-active web") {
			t.Errorf("%s: commands = %q, want the unit resolved from the alias", key, conn.Commands)
		}
	}
}

// A unit name that needs quoting pins shellQuote's part in the command: the
// fixtures above read `systemctl is-active web` with no quotes precisely
// because shellQuote leaves a safe word alone, and a test keyed on the
// quoted form silently matched nothing — which is how the first run of this
// file reported "changed = false" instead of a wrong command.
func TestSystemdQuotesAUnitNameThatNeedsIt(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl is-active 'my unit.service'": {RC: 0},
	})
	if _, err := moduleSystemd(context.Background(), conn, map[string]any{
		"name": "my unit.service", "state": "started",
	}); err != nil {
		t.Fatal(err)
	}
	if !containsCmd(conn.Commands, "systemctl is-active 'my unit.service'") {
		t.Errorf("commands = %q, want the name quoted", conn.Commands)
	}
}

func containsCmd(cmds []string, want string) bool {
	for _, c := range cmds {
		if c == want {
			return true
		}
	}
	return false
}
