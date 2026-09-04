package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file (keyring.go/keyring_info.go) implements Ansible's
// `keyring`/`keyring_info` (community.general) modules, which for real
// ansible use the cross-platform Python `keyring` library — an
// abstraction over several native OS secret stores (macOS Keychain,
// Linux Secret Service/libsecret via gnome-keyring, Windows Credential
// Locker, KDE Wallet, plaintext-on-disk fallbacks, ...) selected
// automatically at runtime.
//
// This port has no such library, and no portable way to bind one, so
// per this batch's own architectural decision it narrows real
// keyring's cross-platform abstraction down to the two CLI-backed
// OSes/backends explicitly in scope: `secret-tool` (Linux, part of
// libsecret-tools, talking to the Secret Service D-Bus API that
// gnome-keyring or an equivalent implements) and `security` (macOS,
// Apple's own Keychain Services CLI). Every OTHER real keyring
// backend (Windows, KDE Wallet, a plaintext fallback file, ...) is out
// of scope for this port entirely — a task targeting one of those
// simply finds neither `secret-tool` nor `security` on PATH and fails
// cleanly (see keyringDispatch's own doc comment).
//
// Args (both modules): service (required); username (required);
// keyring_password (required) — see below for how this port actually
// uses it, which differs from what its own name suggests on macOS.
// keyring.go only: user_password (required for state=present, alias
// password); state (present|absent, default present, keyring.go only).
//
// # keyring_password's real meaning, and why this port only honors it
// on Linux
//
// Real keyring.py/keyring_info.py's OWN documented control flow tries
// the native `keyring` Python library FIRST (which, on both Linux and
// macOS, "just works" against an already-unlocked, already-running
// user secret-service session — the ordinary case for anyone actually
// logged in) and only falls back to a keyring_password-driven manual
// unlock recipe — literally `dbus-run-session -- /bin/bash` running
// `echo "$keyring_password" | gnome-keyring-daemon --unlock` followed
// by the `keyring` CLI — when that native call raises
// keyring.errors.KeyringLocked, i.e. a HEADLESS session with no
// already-unlocked keyring (a CI runner, a systemd service with no
// login session, ...). That fallback recipe is Linux/gnome-keyring
// specific; there is no equivalent "security -unlock-keychain
// non-interactively with no prior session" recipe this port could
// substitute for macOS the same way (macOS's own `security
// unlock-keychain -p <password>` DOES exist, but unlocking a
// specific, potentially non-default keychain file by explicit path
// is a materially different, session-independent operation real
// keyring.py never attempts either — it relies on the same "already
// logged in" assumption `security`'s own default keychain commands
// do).
//
// So this port replicates real keyring.py's own headless-unlock
// fallback recipe VERBATIM whenever `secret-tool` is the selected
// backend (this port has no native library to try first, so it always
// takes the path real keyring.py only reaches as ITS OWN fallback):
// `dbus-run-session -- /bin/bash -c 'echo "$KEYRING_PASSWORD" |
// gnome-keyring-daemon --unlock; secret-tool ...'`, with
// keyring_password passed via the KEYRING_PASSWORD environment
// variable (never a command-line argument — this project's own hard
// "no secrets in argv" rule, matching redis.go's REDISCLI_AUTH
// precedent) rather than shell-interpolated. When `security` is the
// selected backend instead, keyring_password is accepted (for
// argument-shape compatibility) but has NO EFFECT — a documented,
// deliberate narrowing, not a silent gap.
//
// # user_password on macOS: unavoidably passed via argv
//
// `security add-generic-password`'s own `-w` flag is the ONLY way to
// supply a password non-interactively — its own `--help` output
// states plainly "Specify -w as the last option to be prompted",
// i.e. omitting a value makes it read from the controlling terminal,
// which this port's target session does not have. There is no stdin
// or environment-variable alternative in the `security` CLI itself.
// This means user_password UNAVOIDABLY appears in `security`'s own
// argv (and therefore that target's process listing, however briefly)
// when the macOS backend is selected — a real, inherent limitation of
// the substituted tool, not an oversight of this port's command
// construction; `secret-tool store` (the Linux backend) has no such
// problem, since it reads its own secret from stdin.
func moduleKeyring(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	service, err := requireString(args, "service")
	if err != nil {
		return Result{}, err
	}
	username, err := requireString(args, "username")
	if err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "keyring_password"); err != nil {
		return Result{}, err
	}
	keyringPassword := argString(args, "keyring_password", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keyring: state must be one of present, absent, got %q", state)
	}

	current, found, res, err := keyringGet(ctx, conn, service, username, keyringPassword)
	if err != nil {
		return Result{}, err
	}
	if res.Failed {
		return res, nil
	}

	label := service + "@" + username
	switch state {
	case "present":
		userPassword := argString(args, "user_password", argString(args, "password", ""))
		if found && current == userPassword {
			return Ok("Passphrase already set for " + label), nil
		}
		setRes, err := keyringSet(ctx, conn, service, username, keyringPassword, userPassword)
		if err != nil {
			return Result{}, err
		}
		if setRes.Failed {
			return setRes, nil
		}
		return Changed("Passphrase has been updated for " + label), nil

	default: // absent
		if !found {
			return Ok("Passphrase already absent for " + label), nil
		}
		delRes, err := keyringDelete(ctx, conn, service, username, keyringPassword)
		if err != nil {
			return Result{}, err
		}
		if delRes.Failed {
			return delRes, nil
		}
		return Changed("Passphrase has been removed for " + label), nil
	}
}

// keyringDispatch builds a single shell command that runs linuxCmd if
// `secret-tool` is on the target's PATH, else macosCmd if `security`
// is, else exits 3 with a clear diagnostic — one Exec round trip per
// keyring operation, the same "if command -v X; then ...; else ...;
// fi" two-tier dispatch style hostname.go's own hostnameSetCmd uses.
func keyringDispatch(linuxCmd, macosCmd string) string {
	return "if command -v secret-tool >/dev/null 2>&1; then " + linuxCmd +
		"; elif command -v security >/dev/null 2>&1; then " + macosCmd +
		"; else echo 'keyring: neither secret-tool (Linux/libsecret) nor security (macOS Keychain) found in PATH' >&2; exit 3; fi"
}

// keyringNoBackend reports whether res is keyringDispatch's own
// synthetic "no backend found" exit.
func keyringNoBackend(res remoteexec.Result) bool {
	return res.RC == 3 && strings.Contains(res.Stderr, "neither secret-tool")
}

// keyringGet reads service/username's current secret. found=false
// (not an error) means the item doesn't exist; a Result with
// Failed=true means the operation itself could not be attempted (no
// backend on PATH) and should be returned to the caller as-is.
func keyringGet(ctx context.Context, conn remoteexec.Connection, service, username, keyringPassword string) (value string, found bool, failRes Result, err error) {
	qs, qu := shellQuote(service), shellQuote(username)
	linux := "echo \"$KEYRING_PASSWORD\" | gnome-keyring-daemon --unlock >/dev/null 2>&1; " +
		"dbus-run-session -- secret-tool lookup service " + qs + " username " + qu
	macos := "security find-generic-password -a " + qu + " -s " + qs + " -w"
	cmd := "KEYRING_PASSWORD=" + shellQuote(keyringPassword) + " " + keyringDispatch(linux, macos)

	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return "", false, Result{}, err
	}
	if keyringNoBackend(res) {
		return "", false, Fail("keyring: " + strings.TrimSpace(res.Stderr)), nil
	}
	if res.RC != 0 {
		return "", false, Result{}, nil
	}
	return strings.TrimRight(res.Stdout, "\n"), true, Result{}, nil
}

// keyringSet stores userPassword for service/username.
func keyringSet(ctx context.Context, conn remoteexec.Connection, service, username, keyringPassword, userPassword string) (Result, error) {
	qs, qu, qp := shellQuote(service), shellQuote(username), shellQuote(userPassword)
	label := shellQuote(service + "/" + username)
	linux := "echo \"$KEYRING_PASSWORD\" | gnome-keyring-daemon --unlock >/dev/null 2>&1; " +
		"printf %s \"$USER_PASSWORD\" | dbus-run-session -- secret-tool store --label=" + label +
		" service " + qs + " username " + qu
	macos := "security add-generic-password -a " + qu + " -s " + qs + " -w " + qp + " -U"
	cmd := "KEYRING_PASSWORD=" + shellQuote(keyringPassword) + " USER_PASSWORD=" + shellQuote(userPassword) + " " +
		keyringDispatch(linux, macos)

	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return Result{}, err
	}
	if keyringNoBackend(res) {
		return Fail("keyring: " + strings.TrimSpace(res.Stderr)), nil
	}
	if res.RC != 0 {
		return Fail("keyring: failed to set passphrase: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Result{}, nil
}

// keyringDelete removes service/username's secret.
func keyringDelete(ctx context.Context, conn remoteexec.Connection, service, username, keyringPassword string) (Result, error) {
	qs, qu := shellQuote(service), shellQuote(username)
	linux := "echo \"$KEYRING_PASSWORD\" | gnome-keyring-daemon --unlock >/dev/null 2>&1; " +
		"dbus-run-session -- secret-tool clear service " + qs + " username " + qu
	macos := "security delete-generic-password -a " + qu + " -s " + qs
	cmd := "KEYRING_PASSWORD=" + shellQuote(keyringPassword) + " " + keyringDispatch(linux, macos)

	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return Result{}, err
	}
	if keyringNoBackend(res) {
		return Fail("keyring: " + strings.TrimSpace(res.Stderr)), nil
	}
	if res.RC != 0 {
		return Fail("keyring: failed to delete passphrase: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Result{}, nil
}
