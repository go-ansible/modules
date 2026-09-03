package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSupervisorctl implements (a subset of) Ansible's
// `supervisorctl` (community.general) module: manages a program or
// group's state via the `supervisorctl` CLI, idempotent on its current
// status (the same query-then-act shape as systemd.go's `unitIsActive`/
// `unitIsEnabled` checks, but sourced from `supervisorctl status`
// instead of `systemctl is-active`/`is-enabled`).
//
// Args: name (string, required) — a program name, a "group:" name
// (trailing colon), or "all"; state (string, required — one of present,
// started, stopped, restarted, absent, signalled; real supervisorctl's
// own doc lists no default, so this port requires it rather than
// guessing one); config (path) — adds `-c <config>`; username, password
// (string) — add `-u <username> -p <password>`; server_url (string) —
// adds `-s <server_url>`; supervisorctl_path (path, default
// "supervisorctl"); signal (string, required when state=signalled);
// stop_before_removing (bool, default false).
//
// State semantics, matching real supervisorctl's own documented NOTES:
//   - present: `supervisorctl reread` + `add <name>` if not already
//     known to supervisord; does not change its running state.
//   - started: ensured present (as above) if missing, then `start
//     <name>` if not already RUNNING.
//   - stopped: `stop <name>` if currently RUNNING (or STARTING);
//     a program supervisord doesn't know about at all is left alone
//     (nothing to stop) rather than being added first.
//   - restarted: always `supervisorctl update` then `restart <name>`,
//     and always reports Changed — matching real supervisorctl's own
//     documented two-command sequence, and mirroring mount.go's
//     `remounted`/systemd.go-adjacent modules' convention that an
//     unconditional action-verb state always reports changed, since
//     there is no cheap way to tell whether the restart altered
//     anything.
//   - absent: `reread` + `remove <name>`. If the program is currently
//     running: stop_before_removing=true stops it first; otherwise this
//     port fails cleanly (matching real supervisorctl's own documented
//     "the action fails" behavior) rather than attempting the remove
//     and letting supervisorctl itself reject it.
//   - signalled: requires `signal`; runs `signal <signal> <name>` and
//     always reports Changed (a signal is delivered every time by
//     design; there's no idempotent "already signalled" state to check
//     against, matching real supervisorctl's own doc, which documents
//     no idempotency check for this state either).
//
// Idempotency's status check treats any status line for `name` whose
// second field is not "RUNNING"/"STARTING" as "not running", and a
// `status` exit code of nonzero or output containing "no such process"
// / "no such group" as "not present" — a plain, best-effort text check
// over supervisorctl's human-readable output (the same "best-effort,
// documented" convention as debconf.go's own status check), not a
// structured query.
func moduleSupervisorctl(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	stopBeforeRemoving := argBool(args, "stop_before_removing", false)

	base := supervisorctlBaseCmd(args)

	switch state {
	case "present":
		changed, err := supervisorctlEnsurePresent(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if changed {
			return Changed(name + " added"), nil
		}
		return Ok(name + " already present"), nil

	case "started":
		addedChanged, err := supervisorctlEnsurePresent(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		running, _, err := supervisorctlStatus(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if running {
			if addedChanged {
				return Changed(name + " added"), nil
			}
			return Ok(name + " already started"), nil
		}
		if _, err := run(ctx, conn, base+" start "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " started"), nil

	case "stopped":
		running, exists, err := supervisorctlStatus(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if !exists || !running {
			return Ok(name + " already stopped"), nil
		}
		if _, err := run(ctx, conn, base+" stop "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " stopped"), nil

	case "restarted":
		if _, err := run(ctx, conn, base+" update"); err != nil {
			return Result{}, err
		}
		if _, err := run(ctx, conn, base+" restart "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " restarted"), nil

	case "absent":
		running, exists, err := supervisorctlStatus(ctx, conn, base, name)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(name + " already absent"), nil
		}
		if running {
			if !stopBeforeRemoving {
				return Fail(name + " is still running; set stop_before_removing: true, or stop it first"), nil
			}
			if _, err := run(ctx, conn, base+" stop "+shellQuote(name)); err != nil {
				return Result{}, err
			}
		}
		if _, err := run(ctx, conn, base+" reread"); err != nil {
			return Result{}, err
		}
		if _, err := run(ctx, conn, base+" remove "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	case "signalled":
		signal, err := requireString(args, "signal")
		if err != nil {
			return Result{}, errArg("supervisorctl: signal is required when state is signalled")
		}
		if _, err := run(ctx, conn, base+" signal "+shellQuote(signal)+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " signalled " + signal), nil

	default:
		return Result{}, errArg("supervisorctl: state must be one of present, started, stopped, restarted, absent, signalled, got %q", state)
	}
}

func supervisorctlBaseCmd(args map[string]any) string {
	path := argString(args, "supervisorctl_path", "supervisorctl")
	cmd := shellQuote(path)
	if config := argString(args, "config", ""); config != "" {
		cmd += " -c " + shellQuote(config)
	}
	if username := argString(args, "username", ""); username != "" {
		cmd += " -u " + shellQuote(username)
	}
	if password := argString(args, "password", ""); password != "" {
		cmd += " -p " + shellQuote(password)
	}
	if serverURL := argString(args, "server_url", ""); serverURL != "" {
		cmd += " -s " + shellQuote(serverURL)
	}
	return cmd
}

func supervisorctlEnsurePresent(ctx context.Context, conn remoteexec.Connection, base, name string) (bool, error) {
	_, exists, err := supervisorctlStatus(ctx, conn, base, name)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if _, err := run(ctx, conn, base+" reread"); err != nil {
		return false, err
	}
	if _, err := run(ctx, conn, base+" add "+shellQuote(name)); err != nil {
		return false, err
	}
	return true, nil
}

// supervisorctlStatus reports whether name is currently running
// (RUNNING or STARTING) and whether supervisord knows about it at all.
func supervisorctlStatus(ctx context.Context, conn remoteexec.Connection, base, name string) (running, exists bool, err error) {
	res, err := conn.Exec(ctx, base+" status "+shellQuote(name), nil)
	if err != nil {
		return false, false, err
	}
	out := strings.ToLower(res.Stdout + res.Stderr)
	if strings.Contains(out, "no such process") || strings.Contains(out, "no such group") {
		return false, false, nil
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) < 2 {
		return false, res.RC == 0, nil
	}
	status := strings.ToUpper(fields[1])
	running = status == "RUNNING" || status == "STARTING"
	return running, true, nil
}
