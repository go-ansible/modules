package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// These helpers reconstruct the exact command strings keyringGet/Set/
// Delete build internally (see keyring.go), reusing the real
// keyringDispatch helper for the tricky if/elif/else wrapper so the
// dispatch shape itself is never duplicated by hand.
func keyringGetCmdForTest(service, username, keyringPassword string) string {
	qs, qu := shellQuote(service), shellQuote(username)
	linux := "echo \"$KEYRING_PASSWORD\" | gnome-keyring-daemon --unlock >/dev/null 2>&1; " +
		"dbus-run-session -- secret-tool lookup service " + qs + " username " + qu
	macos := "security find-generic-password -a " + qu + " -s " + qs + " -w"
	// ⛔ The command no longer carries the password: it goes in the
	// process environment via ExecWithEnv, because an assignment
	// followed by `;` keeps the shell alive and puts the secret in its
	// argv for `ps` to read. These helpers failing when the prefix was
	// removed is what proved it is gone.
	_ = keyringPassword
	return keyringDispatch(linux, macos)
}

func keyringSetCmdForTest(service, username, keyringPassword, userPassword string) string {
	qs, qu := shellQuote(service), shellQuote(username)
	label := shellQuote(service + "/" + username)
	linux := "echo \"$KEYRING_PASSWORD\" | gnome-keyring-daemon --unlock >/dev/null 2>&1; " +
		"printf %s \"$USER_PASSWORD\" | dbus-run-session -- secret-tool store --label=" + label +
		" service " + qs + " username " + qu
	macos := "security add-generic-password -a " + qu + " -s " + qs + ` -w "$USER_PASSWORD" -U`
	_, _ = keyringPassword, userPassword
	return keyringDispatch(linux, macos)
}

func keyringDeleteCmdForTest(service, username, keyringPassword string) string {
	qs, qu := shellQuote(service), shellQuote(username)
	linux := "echo \"$KEYRING_PASSWORD\" | gnome-keyring-daemon --unlock >/dev/null 2>&1; " +
		"dbus-run-session -- secret-tool clear service " + qs + " username " + qu
	macos := "security delete-generic-password -a " + qu + " -s " + qs
	// ⛔ The command no longer carries the password: it goes in the
	// process environment via ExecWithEnv, because an assignment
	// followed by `;` keeps the shell alive and puts the secret in its
	// argv for `ps` to read. These helpers failing when the prefix was
	// removed is what proved it is gone.
	_ = keyringPassword
	return keyringDispatch(linux, macos)
}

func TestModuleKeyringSetNew(t *testing.T) {
	getCmd := keyringGetCmdForTest("svc", "user", "kpw")
	setCmd := keyringSetCmdForTest("svc", "user", "kpw", "pw")
	conn := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 1},
		setCmd: {RC: 0},
	})
	res, err := moduleKeyring(context.Background(), conn, map[string]any{
		"service": "svc", "username": "user", "keyring_password": "kpw", "user_password": "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeyringSetAlreadyMatches(t *testing.T) {
	getCmd := keyringGetCmdForTest("svc", "user", "kpw")
	conn := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 0, Stdout: "pw"},
	})
	res, err := moduleKeyring(context.Background(), conn, map[string]any{
		"service": "svc", "username": "user", "keyring_password": "kpw", "user_password": "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeyringAbsentFound(t *testing.T) {
	getCmd := keyringGetCmdForTest("svc", "user", "kpw")
	delCmd := keyringDeleteCmdForTest("svc", "user", "kpw")
	conn := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 0, Stdout: "pw"},
		delCmd: {RC: 0},
	})
	res, err := moduleKeyring(context.Background(), conn, map[string]any{
		"service": "svc", "username": "user", "keyring_password": "kpw", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeyringAbsentNotFound(t *testing.T) {
	getCmd := keyringGetCmdForTest("svc", "user", "kpw")
	conn := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 1},
	})
	res, err := moduleKeyring(context.Background(), conn, map[string]any{
		"service": "svc", "username": "user", "keyring_password": "kpw", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeyringNoBackend(t *testing.T) {
	getCmd := keyringGetCmdForTest("svc", "user", "kpw")
	conn := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 3, Stderr: "keyring: neither secret-tool (Linux/libsecret) nor security (macOS Keychain) found in PATH"},
	})
	res, err := moduleKeyring(context.Background(), conn, map[string]any{
		"service": "svc", "username": "user", "keyring_password": "kpw", "user_password": "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeyringMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeyring(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}

// ⛔ SECURITY REGRESSION TEST. keyring built
//
//	KEYRING_PASSWORD=<secret>; USER_PASSWORD=<secret>; <dispatch>
//
// An assignment followed by `;` is two statements, so the shell cannot
// exec away and its argv carries both secrets for the command's whole
// lifetime -- readable by any local user through `ps`. Demonstrated
// upstream, where `/bin/sh -c MY_TOKEN=canary-… sleep 4; true` shows
// the value while a prefix on a LONE command does not.
//
// The assertion is in both directions, because either alone passes for
// the wrong reason: the secrets must be in the environment, AND absent
// from every command string.
func TestKeyringKeepsSecretsOffTheCommandLine(t *testing.T) {
	const kpw, upw = "KEYRING-SECRET-A", "USER-SECRET-B"
	// Every state the module accepts, because the first version of this
	// test drove `present` only -- and keyringDelete, which only `absent`
	// reaches, kept the secret on the command line for another commit.
	// `witness` is what makes each case prove it exercised ITS OWN path:
	// without it a case that silently returned early would pass by
	// finding no secret in no commands at all.
	for _, tc := range []struct {
		state   string
		witness string
	}{
		{"present", "secret-tool store"},
		{"absent", "secret-tool clear"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			conn := newFakeConn(map[string]remoteexec.Result{})
			_, _ = moduleKeyring(context.Background(), conn, map[string]any{
				"service": "svc", "username": "user",
				"keyring_password": kpw, "user_password": upw, "state": tc.state,
			})
			for _, c := range conn.Commands {
				for _, secret := range []string{kpw, upw} {
					if strings.Contains(c, secret) {
						t.Errorf("a secret is in a command string, where ps can read it:\n  %s", c)
					}
				}
			}
			reached := false
			for _, c := range conn.Commands {
				if strings.Contains(c, tc.witness) {
					reached = true
				}
			}
			if !reached {
				t.Fatalf("state %q never reached %q, so this case asserted nothing; commands: %q",
					tc.state, tc.witness, conn.Commands)
			}
			if conn.Envs["KEYRING_PASSWORD"] != kpw {
				t.Errorf("KEYRING_PASSWORD did not reach the environment: %q", conn.Envs["KEYRING_PASSWORD"])
			}
		})
	}
}

// TestKeyringSetSendsTheUserPasswordThroughTheEnvironment is split out
// because only `state: present` has a user password at all: folding it
// into the table above would have meant asserting it for `absent` too,
// where its absence is correct.
func TestKeyringSetSendsTheUserPasswordThroughTheEnvironment(t *testing.T) {
	const kpw, upw = "KEYRING-SECRET-A", "USER-SECRET-B"
	conn := newFakeConn(map[string]remoteexec.Result{})
	_, _ = moduleKeyring(context.Background(), conn, map[string]any{
		"service": "svc", "username": "user",
		"keyring_password": kpw, "user_password": upw, "state": "present",
	})
	if conn.Envs["USER_PASSWORD"] != upw {
		t.Errorf("USER_PASSWORD did not reach the environment: %q", conn.Envs["USER_PASSWORD"])
	}
}
