package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRunit implements Ansible's `runit` (community.general) module:
// controls a runit-supervised service via the `sv` utility — the same
// query-then-act shape as sysvinit.go/supervisorctl.go's own modules,
// but for runit's own status text ("run", "down", ...) instead of an
// init script's exit code or supervisorctl's status column.
//
// Args: name (string, required); state (string, optional — one of
// started, stopped, restarted, reloaded, once, killed; when unset only
// `enabled` is applied, matching systemd.go/sysvinit.go's own
// convention); enabled (bool, optional) — present implies a symlink
// from service_src/name to service_dir/name; absent removes it (and,
// per real runit's own documented "if disabled it also implies
// stopped" behavior, this port also stops the service first when
// disabling one that's running); service_dir (string, default
// "/var/service"); service_src (string, default "/etc/sv").
//
// State semantics, matching real runit's own documented NOTES:
//   - started/stopped: idempotent, checked against `sv status` first
//     (a line beginning "run: " means running, matching real runit's
//     own status-parsing convention).
//   - restarted/killed/reloaded/once: always run (`sv restart`/`sv
//     force-stop`/`sv reload`/`sv once`) and always report Changed —
//     matching real runit's own doc, which explicitly documents these
//     as non-idempotent ("always bounces the service").
//
// Simplifications vs real runit: no distinction between "not yet
// supervised at all" (no symlink in service_dir) and "supervised but
// down" beyond what `sv status` itself reports — real runit's Python
// implementation has similar limits, since sv's own status output is
// the only signal available either way.
func moduleRunit(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "")
	serviceDir := argString(args, "service_dir", "/var/service")
	serviceSrc := argString(args, "service_src", "/etc/sv")

	changed := false

	if _, ok := args["enabled"]; ok {
		wantEnabled := argBool(args, "enabled", false)
		linkPath := serviceDir + "/" + name
		exists, err := pathExists(ctx, conn, linkPath)
		if err != nil {
			return Result{}, err
		}
		if wantEnabled && !exists {
			if _, err := run(ctx, conn, "ln -s "+shellQuote(serviceSrc+"/"+name)+" "+shellQuote(linkPath)); err != nil {
				return Result{}, err
			}
			changed = true
		} else if !wantEnabled && exists {
			running, err := runitIsRunning(ctx, conn, serviceDir, name)
			if err != nil {
				return Result{}, err
			}
			if running {
				if _, err := run(ctx, conn, "sv stop "+shellQuote(serviceDir+"/"+name)); err != nil {
					return Result{}, err
				}
			}
			if _, err := run(ctx, conn, "rm -f "+shellQuote(linkPath)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if state != "" {
		svTarget := shellQuote(serviceDir + "/" + name)
		switch state {
		case "started":
			running, err := runitIsRunning(ctx, conn, serviceDir, name)
			if err != nil {
				return Result{}, err
			}
			if !running {
				if _, err := run(ctx, conn, "sv start "+svTarget); err != nil {
					return Result{}, err
				}
				changed = true
			}
		case "stopped":
			running, err := runitIsRunning(ctx, conn, serviceDir, name)
			if err != nil {
				return Result{}, err
			}
			if running {
				if _, err := run(ctx, conn, "sv stop "+svTarget); err != nil {
					return Result{}, err
				}
				changed = true
			}
		case "restarted":
			if _, err := run(ctx, conn, "sv restart "+svTarget); err != nil {
				return Result{}, err
			}
			changed = true
		case "killed":
			if _, err := run(ctx, conn, "sv force-stop "+svTarget); err != nil {
				return Result{}, err
			}
			changed = true
		case "reloaded":
			if _, err := run(ctx, conn, "sv reload "+svTarget); err != nil {
				return Result{}, err
			}
			changed = true
		case "once":
			if _, err := run(ctx, conn, "sv once "+svTarget); err != nil {
				return Result{}, err
			}
			changed = true
		default:
			return Result{}, errArg("runit: unknown state %q", state)
		}
	}

	if changed {
		return Changed(name), nil
	}
	return Ok(name + " unchanged"), nil
}

// runitIsRunning reports whether `sv status` for name starts with
// "run: " (runit's own status prefix for a currently-running service).
func runitIsRunning(ctx context.Context, conn remoteexec.Connection, serviceDir, name string) (bool, error) {
	res, err := conn.Exec(ctx, "sv status "+shellQuote(serviceDir+"/"+name), nil)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.TrimSpace(res.Stdout), "run:"), nil
}
