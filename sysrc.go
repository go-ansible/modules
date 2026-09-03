package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSysrc implements Ansible's `sysrc` (community.general) module:
// manages a single variable in FreeBSD's /etc/rc.conf (or another file
// in the same syntax) via the `sysrc` binary itself, so this port
// doesn't need to reimplement rc.conf's own shell-variable-assignment
// parsing/quoting rules — it always defers to the real `sysrc` tool for
// both reading and writing, the same "shell out rather than reimplement
// a format" stance mail.go documents for curl/SMTP.
//
// Args: name (string, required — must not contain '.', matching real
// sysrc's own documented restriction, since sysrc has no OID-style
// name support); value (string) — required for state=present/
// value_present, ignored otherwise; state (string, default "present" —
// one of present, absent, value_present, value_absent); path (string,
// default "/etc/rc.conf"); jail (string, optional) — operates inside
// the named jail via `sysrc -j <jail>`; delim (string, default " ") —
// the separator between existing values for value_present/value_absent,
// passed through to sysrc's own `-D <delim>` flag (only when non-
// default, since sysrc's own default delimiter is already a space).
//
// State semantics, matching real sysrc's own documented behavior:
//   - present: `sysrc -f <path> <name>=<value>`; idempotent — first
//     queried via `sysrc -f <path> -n <name>` and skipped if the
//     current value already matches.
//   - absent: `sysrc -f <path> -x <name>`; idempotent — skipped if the
//     name is already unset (checked via the same query, treating a
//     non-zero exit as "unset").
//   - value_present: `sysrc -f <path> <name>+=<value>` (sysrc's own
//     "append if not already one of the space/delim-separated values"
//     operator) with `-D <delim>` when delim is non-default; sysrc
//     itself is idempotent here (a value already present is a no-op),
//     so this port always issues the command and derives Changed by
//     comparing the queried value before and after.
//   - value_absent: `sysrc -f <path> <name>-=<value>` (sysrc's own
//     "remove if present" operator), same before/after comparison for
//     Changed.
//
// Simplifications vs real sysrc: no OID-name validation (a name
// containing '.' is passed straight through to the `sysrc` binary,
// which then rejects it itself — this port doesn't pre-validate and
// duplicate that check).
func moduleSysrc(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	path := argString(args, "path", "/etc/rc.conf")
	jail := argString(args, "jail", "")
	delim := argString(args, "delim", " ")
	value := argString(args, "value", "")

	base := "sysrc -f " + shellQuote(path)
	if jail != "" {
		base += " -j " + shellQuote(jail)
	}

	switch state {
	case "present":
		current, isSet, err := sysrcQuery(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if isSet && current == value {
			return Ok(name + " already set"), nil
		}
		if _, err := run(ctx, conn, base+" "+shellQuote(name+"="+value)); err != nil {
			return Result{}, err
		}
		return Changed(name + " set"), nil

	case "absent":
		_, isSet, err := sysrcQuery(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if !isSet {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, base+" -x "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	case "value_present":
		before, _, err := sysrcQuery(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		cmd := base
		if delim != " " {
			cmd += " -D " + shellQuote(delim)
		}
		if _, err := run(ctx, conn, cmd+" "+shellQuote(name+"+="+value)); err != nil {
			return Result{}, err
		}
		after, _, err := sysrcQuery(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if after == before {
			return Ok(name + ": value already present"), nil
		}
		return Changed(name + ": value added"), nil

	case "value_absent":
		before, isSet, err := sysrcQuery(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if !isSet {
			return Ok(name + " already absent"), nil
		}
		cmd := base
		if delim != " " {
			cmd += " -D " + shellQuote(delim)
		}
		if _, err := run(ctx, conn, cmd+" "+shellQuote(name+"-="+value)); err != nil {
			return Result{}, err
		}
		after, _, err := sysrcQuery(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if after == before {
			return Ok(name + ": value already absent"), nil
		}
		return Changed(name + ": value removed"), nil

	default:
		return Result{}, errArg("sysrc: state must be one of present, absent, value_present, value_absent, got %q", state)
	}
}

// sysrcQuery runs `sysrc -f <path> -n <name>` (via base, which already
// carries -f/-j) and reports name's current value (trimmed) and whether
// it is set at all (RC 0).
func sysrcQuery(ctx context.Context, conn remoteexec.Connection, base, name string) (value string, isSet bool, err error) {
	res, err := conn.Exec(ctx, base+" -n "+shellQuote(name), nil)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	return strings.TrimSpace(res.Stdout), true, nil
}
