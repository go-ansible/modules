package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSystemd implements (a subset of) Ansible's `systemd_service` module
// (`systemd` and `service` are its own registered aliases).
//
// Args: name (aliases service, unit) — required only by state, enabled and
// masked, NOT by the module; state (started|stopped|restarted|reloaded);
// enabled (bool); daemon_reload (bool, alias daemon-reload, default false);
// daemon_reexec (bool, alias daemon-reexec, default false); scope
// (system|user|global, default system).
//
// # `name` is not required; `daemon_reload` alone is a valid task
//
// Real's argument_spec (ansible-core 2.21.4, `modules/systemd_service.py:344`)
// does not mark `name` required. It declares instead:
//
//	required_one_of=[['state', 'enabled', 'masked', 'daemon_reload', 'daemon_reexec']]
//	required_by=dict(state=('name',), enabled=('name',), masked=('name',))
//
// so `systemd_service: daemon_reload=true` with no unit is the documented way
// to re-read unit files after dropping one in, and it is what the real module
// is most often used for in that position. This port refused it until now
// (go-ansible/modules#108) because it asked for `name` unconditionally, and
// ignored `daemon_reload` entirely when it was given (#107) — the argument was
// accepted and silently did nothing, which is the worst of the three possible
// behaviours.
//
// A parameter counts as given when its key is present with a non-null value,
// which is what real's own `required_one_of` sees: it is checked BEFORE
// defaults are applied (`common/arg_spec.py` runs `_ADDITIONAL_CHECKS` at
// line 243 and `_set_defaults` at 249), so `daemon_reload: false` written out
// satisfies it while an omitted `daemon_reload` does not.
//
// # daemon-reload runs first, and does not report changed
//
// Real runs daemon-reload before anything else, then daemon-reexec, then the
// unit work (`systemd_service.py:396-412`), and neither sets `changed`:
// `result['changed'] = True` appears only inside its `if unit:` block, at
// lines 487, 539 and 571. A task that only reloads the manager therefore
// reports ok, not changed. That is reproduced rather than improved on: a
// playbook with `changed_when` or a handler keyed on this task behaves the
// same way here.
//
// # Stated gaps, so they are visible rather than discovered
//
//   - `masked` is in real's required_one_of and is NOT implemented here. It is
//     refused by name rather than accepted and ignored, which is the mistake
//     #107 was.
//   - `force` and `no_block` are accepted and ignored, as before this change.
//   - Real downgrades a failed daemon-reload to a warning when the target is a
//     chroot or `SYSTEMD_OFFLINE=1`. The environment half is reproduced (read
//     on the target, where real reads it); chroot detection is not, so inside
//     a chroot this fails where real continues.
//   - The module is not in `checkModeSupport`, so the engine skips it in a dry
//     run and real's own `and not module.check_mode` guard has nothing to
//     correspond to here. No check-mode branch is written for a path nothing
//     can reach.
func moduleSystemd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	given := func(keys ...string) bool {
		for _, k := range keys {
			if v, ok := args[k]; ok && v != nil {
				return true
			}
		}
		return false
	}

	// required_one_of, in real's wording.
	if !given("state", "enabled", "masked", "daemon_reload", "daemon-reload", "daemon_reexec", "daemon-reexec") {
		return Fail("one of the following is required: state, enabled, masked, daemon_reload, daemon_reexec"), nil
	}
	if given("masked") {
		return Fail("systemd: masked is not implemented by this port — it is in real's own " +
			"required_one_of, so it is refused by name rather than accepted and ignored"), nil
	}

	name := argString(args, "name", "")
	if name == "" {
		name = argString(args, "service", "")
	}
	if name == "" {
		name = argString(args, "unit", "")
	}

	// required_by: state, enabled and masked each need a name.
	for _, k := range []string{"state", "enabled", "masked"} {
		if given(k) && name == "" {
			return Fail(fmt.Sprintf("missing parameter(s) required by '%s': name", k)), nil
		}
	}

	// Real refuses a glob before doing anything (systemd_service.py:366-369).
	if name != "" {
		for _, pattern := range []string{"*", "?", "["} {
			if strings.Contains(name, pattern) {
				return Fail(fmt.Sprintf("This module does not currently support using glob patterns, "+
					"found '%s' in service name: %s", pattern, name)), nil
			}
		}
	}

	// scope is not a flag when it is the default: real appends nothing for
	// "system" and `--user`/`--global` otherwise (systemd_service.py:377-380).
	// It is applied to every systemctl call below, including daemon-reload:
	// reloading the system manager for a task that said `scope: user` would
	// be the wrong manager, silently.
	systemctl := "systemctl"
	switch scope := argString(args, "scope", "system"); scope {
	case "", "system":
	case "user", "global":
		systemctl += " --" + scope
	default:
		return Fail(fmt.Sprintf("value of scope must be one of: system, user, global, got: %s", scope)), nil
	}

	if argBool(args, "daemon_reload", false) || argBool(args, "daemon-reload", false) {
		if res, ok := systemdManagerCmd(ctx, conn, systemctl, "daemon-reload"); !ok {
			return res, nil
		}
	}
	if argBool(args, "daemon_reexec", false) || argBool(args, "daemon-reexec", false) {
		if res, ok := systemdManagerCmd(ctx, conn, systemctl, "daemon-reexec"); !ok {
			return res, nil
		}
	}

	// Nothing else to do: a manager-only task. Real leaves changed false and
	// returns its (empty) status dict.
	if name == "" {
		return Ok(""), nil
	}

	changed := false

	if state := argString(args, "state", ""); state != "" {
		active, err := unitIsActive(ctx, conn, systemctl, name)
		if err != nil {
			return Result{}, err
		}
		var verb string
		switch state {
		case "started":
			if !active {
				verb = "start"
			}
		case "stopped":
			if active {
				verb = "stop"
			}
		case "restarted":
			verb = "restart"
		case "reloaded":
			verb = "reload"
		default:
			return Fail(fmt.Sprintf("value of state must be one of: reloaded, restarted, started, stopped, got: %s", state)), nil
		}
		if verb != "" {
			if _, err := run(ctx, conn, systemctl+" "+verb+" "+shellQuote(name)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if given("enabled") {
		wantEnabled := argBool(args, "enabled", false)
		isEnabled, err := unitIsEnabled(ctx, conn, systemctl, name)
		if err != nil {
			return Result{}, err
		}
		if wantEnabled != isEnabled {
			verb := "disable"
			if wantEnabled {
				verb = "enable"
			}
			if _, err := run(ctx, conn, systemctl+" "+verb+" "+shellQuote(name)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if changed {
		return Changed(name), nil
	}
	return Ok(name + " unchanged"), nil
}

// systemdManagerCmd runs one manager-wide subcommand (daemon-reload or
// daemon-reexec) and reports real's own failure sentence.
//
// Real's leniency when the target is offline is reproduced for the half that
// can be: `SYSTEMD_OFFLINE=1` is read ON THE TARGET, where real's own
// os.environ lookup happens, because that is the host whose systemd is
// offline. Its other half, is_chroot, is not detected here — see the doc
// comment on moduleSystemd.
func systemdManagerCmd(ctx context.Context, conn remoteexec.Connection, systemctl, sub string) (Result, bool) {
	res, err := runStatus(ctx, conn, systemctl+" "+sub)
	if err != nil {
		return Fail(fmt.Sprintf("systemd: running %s %s: %v", systemctl, sub, err)), false
	}
	if res.RC == 0 {
		return Result{}, true
	}
	offline, oerr := targetEnv(ctx, conn, "SYSTEMD_OFFLINE")
	if oerr == nil && offline == "1" {
		// Real warns and continues. This port has no warning channel, so
		// the run continues silently rather than inventing one.
		return Result{}, true
	}
	return Fail(fmt.Sprintf("failure %d during %s: %s", res.RC, sub, strings.TrimSpace(res.Stderr))), false
}

func unitIsActive(ctx context.Context, conn remoteexec.Connection, systemctl, name string) (bool, error) {
	res, err := conn.Exec(ctx, systemctl+" is-active "+shellQuote(name), nil)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", name, err)
	}
	return res.RC == 0, nil
}

func unitIsEnabled(ctx context.Context, conn remoteexec.Connection, systemctl, name string) (bool, error) {
	res, err := conn.Exec(ctx, systemctl+" is-enabled "+shellQuote(name), nil)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", name, err)
	}
	return res.RC == 0, nil
}
