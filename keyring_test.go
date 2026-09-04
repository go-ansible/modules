package modules

import (
	"context"
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
	return "KEYRING_PASSWORD=" + shellQuote(keyringPassword) + "; " + keyringDispatch(linux, macos)
}

func keyringSetCmdForTest(service, username, keyringPassword, userPassword string) string {
	qs, qu, qp := shellQuote(service), shellQuote(username), shellQuote(userPassword)
	label := shellQuote(service + "/" + username)
	linux := "echo \"$KEYRING_PASSWORD\" | gnome-keyring-daemon --unlock >/dev/null 2>&1; " +
		"printf %s \"$USER_PASSWORD\" | dbus-run-session -- secret-tool store --label=" + label +
		" service " + qs + " username " + qu
	macos := "security add-generic-password -a " + qu + " -s " + qs + " -w " + qp + " -U"
	return "KEYRING_PASSWORD=" + shellQuote(keyringPassword) + "; USER_PASSWORD=" + shellQuote(userPassword) + "; " +
		keyringDispatch(linux, macos)
}

func keyringDeleteCmdForTest(service, username, keyringPassword string) string {
	qs, qu := shellQuote(service), shellQuote(username)
	linux := "echo \"$KEYRING_PASSWORD\" | gnome-keyring-daemon --unlock >/dev/null 2>&1; " +
		"dbus-run-session -- secret-tool clear service " + qs + " username " + qu
	macos := "security delete-generic-password -a " + qu + " -s " + qs
	return "KEYRING_PASSWORD=" + shellQuote(keyringPassword) + "; " + keyringDispatch(linux, macos)
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
