package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDconf implements (a subset of) Ansible's `dconf`
// (community.general) module: reads or writes a GNOME `dconf` database
// key via the `dconf` CLI tool — read from real dconf.py's own
// DconfPreference/DBusWrapper classes (this batch's hard rule: the
// D-Bus session discovery and value-comparison fallback below are only
// visible in the implementation, not EXAMPLES/OPTIONS).
//
// Args: key (string, required); state (read|present|absent, default
// "present"); value (raw, required for state=present) — stringified
// via this package's own argString (fmt.Sprint fallback for non-string
// types), which happens to already match real dconf's own bool
// handling (Go's fmt.Sprint(true) is "true"/"false", the exact strings
// real dconf's own main() special-cases a Python bool into) without
// needing a separate case.
//
// Value comparison: real dconf can use the `gi.repository.GLib`
// Python library to parse both the database's current GVariant text
// and the user's given value under the SAME inferred type before
// comparing, so that e.g. "true" and a YAML boolean literal compare
// equal despite differing surface syntax; when that library is
// unavailable, real dconf's own documented fallback is a plain string
// comparison, which it warns "may lead to false positives" (Ansible
// may think a value changed when it did not). This port has no GVariant
// parser at all (no Go equivalent of gi.repository is available), so it
// ALWAYS takes real dconf's own degraded fallback path: a plain string
// comparison between `dconf read`'s own trimmed output and the given
// value. A value that's semantically identical but spelled differently
// (extra whitespace inside a GVariant tuple, "true" vs "'true'", etc.)
// will read as changed here even when real dconf (with gi.repository
// present) would correctly see no change.
//
// D-Bus session discovery: `dconf write`/`dconf reset` need a working
// D-Bus session. Real DBusWrapper tries, in order: (1) the current
// process's own DBUS_SESSION_BUS_ADDRESS environment variable, (2) the
// canonical `/run/user/<uid>/bus` socket path (systemd/dbus-broker),
// each validated with `dbus-send`/`busctl`, then (3) a scan of every
// other process owned by the same uid for one that has
// DBUS_SESSION_BUS_ADDRESS set in ITS OWN environment (via the
// `psutil` library), and finally falls back to `dbus-run-session`. This
// port reproduces steps (1) and (2) — the two cases real dconf's own
// comment already calls out as covering "systemd and dbus-broker",
// which is the overwhelming majority of modern Linux targets — but NOT
// step (3): reading arbitrary other processes' environ requires
// process-inspection privileges this port has no portable, library-free
// shell equivalent for (psutil itself needs root or matching uid, and
// even then a shell-only substitute would mean grepping every
// /proc/<pid>/environ, which is fragile and typically permission-denied
// across users). It also skips the dbus-send/busctl address VALIDATION
// step (just checks the socket path exists), and always falls back
// straight to `dbus-run-session` when neither (1) nor (2) pans out,
// requiring that binary to be on the target's PATH (matching real
// dconf's own required=True get_bin_path for it in that same fallback
// case).
//
// This module does not localize command output (no LANGUAGE=C/LC_ALL=C
// environment override, since remoteexec.Connection's Exec has no
// per-call environment parameter) — a purely cosmetic difference
// affecting only human-readable error text, not value parsing.
func moduleDconf(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "read" && state != "present" && state != "absent" {
		return Result{}, errArg("dconf: state must be read, present, or absent, got %q", state)
	}

	if _, err := run(ctx, conn, "command -v dconf"); err != nil {
		return Fail("dconf: dconf executable not found on the target"), nil
	}

	switch state {
	case "read":
		val, failMsg, err := dconfRead(ctx, conn, key)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		var out any
		if val != nil {
			out = *val
		}
		return Ok("").WithExtra("value", out), nil

	case "present":
		if _, ok := args["value"]; !ok {
			return Result{}, errArg("dconf: value is required when state is present")
		}
		value := argString(args, "value", "")

		cur, failMsg, err := dconfRead(ctx, conn, key)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		if cur != nil && *cur == value {
			return Ok(""), nil
		}

		prefix, ferr, err := dconfDbusPrefix(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if ferr != "" {
			return Fail(ferr), nil
		}
		res, err := runStatus(ctx, conn, prefix+"dconf write "+shellQuote(key)+" "+shellQuote(value))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("dconf failed while writing key " + key + ", value " + value + " with error: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(""), nil

	default: // absent
		cur, failMsg, err := dconfRead(ctx, conn, key)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		if cur == nil {
			return Ok(""), nil
		}

		prefix, ferr, err := dconfDbusPrefix(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if ferr != "" {
			return Fail(ferr), nil
		}
		res, err := runStatus(ctx, conn, prefix+"dconf reset "+shellQuote(key))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("dconf failed while resetting the value with error: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(""), nil
	}
}

// dconfRead runs `dconf read <key>` and returns its value (nil if the
// key is unset in the database, matching real DconfPreference.read's
// own None for empty output), or a non-empty failMsg mirroring real
// dconf's own fail_json wording for a non-zero exit.
func dconfRead(ctx context.Context, conn remoteexec.Connection, key string) (value *string, failMsg string, err error) {
	res, err := runStatus(ctx, conn, "dconf read "+shellQuote(key))
	if err != nil {
		return nil, "", err
	}
	if res.RC != 0 {
		return nil, "dconf failed while reading the value with error: " + strings.TrimSpace(res.Stderr), nil
	}
	if res.Stdout == "" {
		return nil, "", nil
	}
	v := strings.TrimRight(res.Stdout, "\n")
	return &v, "", nil
}

// dconfDbusPrefix returns a shell command prefix that ensures
// `dconf write`/`dconf reset` runs against a working D-Bus session —
// either "DBUS_SESSION_BUS_ADDRESS=<addr> " to reuse a discovered
// session, or "dbus-run-session -- " to spawn one — per this module's
// doc comment. A non-empty failMsg means dbus-run-session is required
// (no existing session found) but not present on the target.
func dconfDbusPrefix(ctx context.Context, conn remoteexec.Connection) (prefix string, failMsg string, err error) {
	// Step 1: DBUS_SESSION_BUS_ADDRESS already set in the shell's own
	// environment (e.g. an interactive/login connection).
	addr, err := run(ctx, conn, `printf '%s' "$DBUS_SESSION_BUS_ADDRESS"`)
	if err != nil {
		return "", "", err
	}
	if addr != "" {
		return "DBUS_SESSION_BUS_ADDRESS=" + shellQuote(addr) + " ", "", nil
	}

	// Step 2: canonical systemd/dbus-broker socket path.
	uid, err := run(ctx, conn, "id -u")
	if err == nil && uid != "" {
		canonical := "/run/user/" + uid + "/bus"
		exists, err := pathExists(ctx, conn, canonical)
		if err != nil {
			return "", "", err
		}
		if exists {
			return "DBUS_SESSION_BUS_ADDRESS=" + shellQuote("unix:path="+canonical) + " ", "", nil
		}
	}

	// Step 3 (process-environ scan) is not reproduced here — see this
	// module's own doc comment. Fall back to dbus-run-session.
	if _, err := run(ctx, conn, "command -v dbus-run-session"); err != nil {
		return "", "dconf: no running D-Bus session found and dbus-run-session is not available on the target", nil
	}
	return "dbus-run-session -- ", "", nil
}
