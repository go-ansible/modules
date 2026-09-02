package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSysvinit implements (a narrow subset of) Ansible's `sysvinit`
// module: manages a service via its /etc/init.d/<name> script — the
// fallback for hosts that don't have systemd (systemd.go's module,
// which this port's `service`/`systemd`/`systemd_service` names all
// resolve to). Real sysvinit's whole value is coping with the wide
// variety of init-script quality found in the wild (scripts with no
// `status` verb, daemons that need `daemonize`d supervision, distro-
// specific enable/disable tooling); this port covers only the common,
// well-behaved case.
//
// Args: name (string, required); state (started|stopped|restarted|
// reloaded, optional — like systemd.go, when unset only `enabled` is
// applied); enabled (bool, optional); pattern (string, optional) — a
// substring to grep for in `ps` output, used as the "is it running"
// check INSTEAD of `/etc/init.d/<name> status` for init scripts that
// don't implement status (matching real sysvinit's own documented
// purpose for this argument); sleep (int, default 1) — seconds to wait
// between an explicit stop and start when state=restarted; arguments
// (string, optional) — appended verbatim to the init script invocation.
//
// Simplifications vs real sysvinit: no `daemonize` (this port never
// double-forks/supervises anything itself — a module here always runs
// on the control node, see module.go's package doc comment, so
// "holding the tty" doesn't apply the same way) and no `runlevels`
// override (enable/disable always targets whatever chkconfig/
// update-rc.d's own defaults are). Unlike moduleSystemd's is-enabled
// check, enabling/disabling here is NOT idempotency-checked — this
// port always runs the enable/disable command and reports changed,
// since reliably detecting "already enabled" differs by which of
// chkconfig or update-rc.d is present and isn't cheap to unify; the
// same "can't cheaply tell already-there apart, so always act, which
// is safe but not idempotent-in-reporting" tradeoff apt_repository's
// PPA path makes elsewhere in this package.
func moduleSysvinit(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "")
	pattern := argString(args, "pattern", "")
	arguments := argString(args, "arguments", "")
	sleep := argInt(args, "sleep", 1)

	changed := false

	if state != "" {
		switch state {
		case "started":
			active, err := sysvinitIsActive(ctx, conn, name, pattern)
			if err != nil {
				return Result{}, err
			}
			if !active {
				if _, err := run(ctx, conn, sysvinitCmd(name, "start", arguments)); err != nil {
					return Result{}, err
				}
				changed = true
			}

		case "stopped":
			active, err := sysvinitIsActive(ctx, conn, name, pattern)
			if err != nil {
				return Result{}, err
			}
			if active {
				if _, err := run(ctx, conn, sysvinitCmd(name, "stop", arguments)); err != nil {
					return Result{}, err
				}
				changed = true
			}

		case "restarted":
			if _, err := runStatus(ctx, conn, sysvinitCmd(name, "stop", arguments)); err != nil {
				return Result{}, err
			}
			if sleep > 0 {
				if _, err := run(ctx, conn, fmt.Sprintf("sleep %d", sleep)); err != nil {
					return Result{}, err
				}
			}
			if _, err := run(ctx, conn, sysvinitCmd(name, "start", arguments)); err != nil {
				return Result{}, err
			}
			changed = true

		case "reloaded":
			if _, err := run(ctx, conn, sysvinitCmd(name, "reload", arguments)); err != nil {
				return Result{}, err
			}
			changed = true

		default:
			return Result{}, errArg("sysvinit: unknown state %q", state)
		}
	}

	if _, ok := args["enabled"]; ok {
		wantEnabled := argBool(args, "enabled", false)
		if err := sysvinitSetEnabled(ctx, conn, name, wantEnabled); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed(name), nil
	}
	return Ok(name + " unchanged"), nil
}

// sysvinitCmd builds an `/etc/init.d/<name> <verb> [arguments]`
// invocation.
func sysvinitCmd(name, verb, arguments string) string {
	cmd := "/etc/init.d/" + shellQuote(name) + " " + verb
	if arguments != "" {
		cmd += " " + arguments
	}
	return cmd
}

// sysvinitIsActive reports whether name is running: via `pattern`
// against `ps` output when given, otherwise via the init script's own
// `status` verb (RC 0 = running).
func sysvinitIsActive(ctx context.Context, conn remoteexec.Connection, name, pattern string) (bool, error) {
	if pattern != "" {
		res, err := conn.Exec(ctx, "ps -ef 2>/dev/null | grep -F "+shellQuote(pattern)+" | grep -qv grep", nil)
		if err != nil {
			return false, fmt.Errorf("checking %s via pattern: %w", name, err)
		}
		return res.RC == 0, nil
	}
	res, err := conn.Exec(ctx, "/etc/init.d/"+shellQuote(name)+" status >/dev/null 2>&1", nil)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", name, err)
	}
	return res.RC == 0, nil
}

// sysvinitSetEnabled enables or disables name at boot via chkconfig
// (RHEL-family) if present, else update-rc.d (Debian-family).
func sysvinitSetEnabled(ctx context.Context, conn remoteexec.Connection, name string, enabled bool) error {
	res, err := conn.Exec(ctx, "command -v chkconfig >/dev/null 2>&1", nil)
	if err != nil {
		return err
	}
	if res.RC == 0 {
		verb := "off"
		if enabled {
			verb = "on"
		}
		_, err := run(ctx, conn, "chkconfig "+shellQuote(name)+" "+verb)
		return err
	}
	if enabled {
		_, err := run(ctx, conn, "update-rc.d "+shellQuote(name)+" enable")
		return err
	}
	_, err = run(ctx, conn, "update-rc.d "+shellQuote(name)+" disable")
	return err
}
