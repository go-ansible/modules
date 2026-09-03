package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOpenwrtInit implements Ansible's `openwrt_init` module
// (community.general): manages an OpenWrt service via its
// `/etc/init.d/<name>` init script, whose protocol differs from
// SysV/systemd — it takes `enabled`/`enable`/`disable`/`running`/
// `start`/`stop`/`restart`/`reload` as its own first argv word,
// answered purely via exit code (no separate query command like
// `systemctl is-enabled`).
//
// Args: name (string, required, alias service); state (started|
// stopped|restarted|reloaded, optional); enabled (bool, optional); at
// least one of state/enabled is required (matching real
// openwrt_init.py's own `required_one_of`); pattern (string, optional)
// — when the init script doesn't support its own `running` command,
// name a substring to search for in `ps w`'s output instead, as a
// stand-in for "is it running".
//
// The init script's existence is checked first (`test -e
// /etc/init.d/<name>`); a missing script is a Fail (matching real
// openwrt_init.py's own fail_json for this case — a well-formed
// request the target simply can't satisfy). Enabled-state is read via
// `<script> enabled` (rc==0 means enabled); toggling it runs `<script>
// enable`/`<script> disable` WITHOUT checking that command's own exit
// code — real openwrt_init.py explicitly ignores it too ("openwrt init
// scripts can return a non-zero exit code on a successful 'enable'
// command if the init script doesn't contain a STOP value") and
// instead re-queries `enabled` afterward to confirm the change stuck,
// which this port replicates exactly. state=started/stopped are
// idempotent (only run `start`/`stop` when not already in that state,
// per `running`/`pattern`); state=restarted/reloaded always run
// `restart`/`reload` unconditionally and are always reported Changed
// — matching real openwrt_init.py's own documented "always bounces the
// service" / "always reloads".
func moduleOpenwrtInit(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", argString(args, "service", ""))
	if name == "" {
		return Result{}, errArg("openwrt_init: missing required argument: name (or its alias service)")
	}
	state := argString(args, "state", "")
	_, enabledSet := args["enabled"]
	enabledWant := argBool(args, "enabled", false)
	if state == "" && !enabledSet {
		return Result{}, errArg("openwrt_init: one of state or enabled is required")
	}
	pattern := argString(args, "pattern", "")

	initScript := "/etc/init.d/" + name
	exists, err := pathExists(ctx, conn, initScript)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail(fmt.Sprintf("service %s does not exist", name)), nil
	}

	changed := false
	var msgs []string

	if enabledSet {
		curEnabled, err := openwrtInitEnabled(ctx, conn, initScript)
		if err != nil {
			return Result{}, err
		}
		if curEnabled != enabledWant {
			changed = true
			action := "disable"
			if enabledWant {
				action = "enable"
			}
			if _, err := run(ctx, conn, shellQuote(initScript)+" "+action); err != nil {
				return Result{}, err
			}
			newEnabled, err := openwrtInitEnabled(ctx, conn, initScript)
			if err != nil {
				return Result{}, err
			}
			if newEnabled != enabledWant {
				return Fail(fmt.Sprintf("Unable to %s service %s", action, name)), nil
			}
			msgs = append(msgs, action+"d")
		}
	}

	if state != "" {
		var action string
		switch state {
		case "started":
			running, err := openwrtInitRunning(ctx, conn, initScript, pattern)
			if err != nil {
				return Result{}, err
			}
			if !running {
				action = "start"
				changed = true
			}
		case "stopped":
			running, err := openwrtInitRunning(ctx, conn, initScript, pattern)
			if err != nil {
				return Result{}, err
			}
			if running {
				action = "stop"
				changed = true
			}
		case "restarted", "reloaded":
			action = strings.TrimSuffix(state, "ed")
			changed = true
		default:
			return Result{}, errArg("openwrt_init: state must be started, stopped, restarted, or reloaded, got %q", state)
		}
		if action != "" {
			res, err := runStatus(ctx, conn, shellQuote(initScript)+" "+action)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail(fmt.Sprintf("Unable to %s service %s: %s", action, name, strings.TrimSpace(res.Stderr))), nil
			}
			msgs = append(msgs, action+"ed")
		}
	}

	if changed {
		return Changed(strings.Join(msgs, "; ")).WithExtra("name", name), nil
	}
	return Ok(name+" unchanged").WithExtra("name", name), nil
}

func openwrtInitEnabled(ctx context.Context, conn remoteexec.Connection, initScript string) (bool, error) {
	res, err := runStatus(ctx, conn, shellQuote(initScript)+" enabled")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

func openwrtInitRunning(ctx context.Context, conn remoteexec.Connection, initScript, pattern string) (bool, error) {
	if pattern != "" {
		res, err := runStatus(ctx, conn, "ps w")
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, nil
		}
		for _, line := range splitLines(res.Stdout) {
			if strings.Contains(line, pattern) && !strings.Contains(line, "pattern=") {
				return true, nil
			}
		}
		return false, nil
	}
	res, err := runStatus(ctx, conn, shellQuote(initScript)+" running")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
