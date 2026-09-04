package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSvc implements Ansible's `svc` (community.general) module:
// controls a daemontools-supervised service via the `svc`/`svstat`
// utilities — daemontools' OWN service-control tool, distinct from
// runit.go's `sv` (runit reuses daemontools' naming for some of its
// own commands, but runit.go's module talks to runit's `sv`/`svstat`,
// not this one).
//
// Args: name (string, required); state (string, optional — one of
// killed, once, reloaded, restarted, started, stopped); enabled (bool,
// optional) — present implies a symlink from service_src/name to
// service_dir/name; absent removes it and, per real svc's own
// documented "if disabled it also implies state=stopped" behavior,
// also runs `svc -dx` on the source directory (and its `log`
// subdirectory, if any); downed (bool, optional) — presence of a
// `down` file inside service_dir/name, which disables auto-restart
// without implying stopped (matching real svc's own documented
// "Downed does not imply stopped"); service_dir (default "/service");
// service_src (default "/etc/service").
//
// Actions run in the same order real svc's own module applies them —
// enabled, then state, then downed — and state's idempotency check is
// made against the service's status as read ONCE at the start (via
// `svstat`, before any of this call's own actions ran), exactly
// matching real svc's own module, which captures Svc(module) state
// once and never re-queries it mid-run.
//
// State semantics, matching real svc's own documented NOTES:
//   - started/stopped: idempotent, skipped when `svstat`'s own output
//     already reports the target state (a line containing " up "/
//     " down ", with an additional "ing" suffix while a " want "
//     transition is still pending, matching real svc's own status
//     parsing).
//   - restarted/killed/reloaded/once: always run (`svc -t`/`svc -k`/
//     `svc -1`/`svc -o`) and always report Changed, matching real
//     svc's own doc ("restarted always bounces the svc... killed
//     always bounces the svc... once... not really an idempotent
//     operation").
//
// Deviation from real svc: real svc's own module dispatches state by
// Python string slicing (`getattr(svc, state[:-2])()`), which strips a
// trailing 2 characters assuming an "...ed" suffix (started->start,
// stopped->stopp, restarted->restart, killed->kill, reloaded->reload).
// For state=once ("once"[:-2] == "on"), there is no such method on
// real svc's own Svc class, so the real module raises an
// AttributeError instead of ever running `svc -o` — despite `once`
// being a documented, listed choice with its own documented behavior
// ("runs a normally downed svc once (svc -o)"). This looks like an
// unintended bug in real svc's own module rather than documented
// intent, so this port implements state=once as `svc -o`, matching
// its own documentation, rather than reproducing a crash.
func moduleSvc(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "")
	if state != "" {
		switch state {
		case "killed", "once", "reloaded", "restarted", "started", "stopped":
		default:
			return Result{}, errArg("svc: state must be one of killed, once, reloaded, restarted, started, stopped, got %q", state)
		}
	}
	serviceDir := argString(args, "service_dir", "/service")
	serviceSrc := argString(args, "service_src", "/etc/service")
	svcFull := serviceDir + "/" + name
	srcFull := serviceSrc + "/" + name

	enabled, err := pathExists(ctx, conn, svcFull)
	if err != nil {
		return Result{}, err
	}

	var downed bool
	var curState string
	if enabled {
		downed, err = pathExists(ctx, conn, svcFull+"/down")
		if err != nil {
			return Result{}, err
		}
		curState, err = svcStatus(ctx, conn, svcFull)
		if err != nil {
			return Result{}, err
		}
	} else {
		downed, err = pathExists(ctx, conn, srcFull+"/down")
		if err != nil {
			return Result{}, err
		}
		curState = "stopped"
	}

	changed := false

	if _, ok := args["enabled"]; ok {
		wantEnabled := argBool(args, "enabled", false)
		if wantEnabled && !enabled {
			exists, err := pathExists(ctx, conn, srcFull)
			if err != nil {
				return Result{}, err
			}
			if !exists {
				return Fail("Could not find source for service to enable ("+srcFull+").").WithExtra("name", name), nil
			}
			if _, err := run(ctx, conn, "ln -s "+shellQuote(srcFull)+" "+shellQuote(svcFull)); err != nil {
				return Result{}, err
			}
			changed = true
		} else if !wantEnabled && enabled {
			if _, err := run(ctx, conn, "rm "+shellQuote(svcFull)); err != nil {
				return Result{}, err
			}
			if _, err := run(ctx, conn, "svc -dx "+shellQuote(srcFull)); err != nil {
				return Result{}, err
			}
			srcLog := srcFull + "/log"
			logExists, err := pathExists(ctx, conn, srcLog)
			if err != nil {
				return Result{}, err
			}
			if logExists {
				if _, err := run(ctx, conn, "svc -dx "+shellQuote(srcLog)); err != nil {
					return Result{}, err
				}
			}
			changed = true
		}
	}

	if state != "" {
		runIt := true
		if state == "started" || state == "stopped" {
			runIt = curState != state
		}
		if runIt {
			flag := map[string]string{
				"started": "-u", "stopped": "-d", "restarted": "-t",
				"killed": "-k", "reloaded": "-1", "once": "-o",
			}[state]
			if _, err := run(ctx, conn, "svc "+flag+" "+shellQuote(svcFull)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if _, ok := args["downed"]; ok {
		wantDowned := argBool(args, "downed", false)
		if wantDowned != downed {
			downFile := svcFull + "/down"
			if wantDowned {
				if _, err := run(ctx, conn, "touch "+shellQuote(downFile)); err != nil {
					return Result{}, err
				}
			} else {
				if _, err := run(ctx, conn, "rm -f "+shellQuote(downFile)); err != nil {
					return Result{}, err
				}
			}
			changed = true
		}
	}

	if changed {
		return Changed(name), nil
	}
	return Ok(name + " unchanged"), nil
}

// svcStatus runs `svstat svcFull` and classifies its output the way
// real svc's own Svc.get_status() does: " up " -> "start", " down " ->
// "stopp", neither -> "unknown" (returned as-is, with no "ed"/"ing"
// suffix — matching real svc's own early return for the unknown case);
// otherwise a trailing "ing" is appended while a " want " transition is
// still pending, else "ed".
func svcStatus(ctx context.Context, conn remoteexec.Connection, svcFull string) (string, error) {
	res, err := runStatus(ctx, conn, "svstat "+shellQuote(svcFull))
	if err != nil {
		return "", err
	}
	out := res.Stdout
	base := "unknown"
	switch {
	case strings.Contains(out, " up "):
		base = "start"
	case strings.Contains(out, " down "):
		base = "stopp"
	}
	if base == "unknown" {
		return "unknown", nil
	}
	if strings.Contains(out, " want ") {
		return base + "ing", nil
	}
	return base + "ed", nil
}
