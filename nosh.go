package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// noshRun runs `system-control [--user] <args...>` on conn, matching
// real nosh.py's own run_sys_ctl() exactly.
func noshRun(ctx context.Context, conn remoteexec.Connection, user bool, argv []string) (remoteexec.Result, error) {
	full := []string{"system-control"}
	if user {
		full = append(full, "--user")
	}
	full = append(full, argv...)
	quoted := make([]string, len(full))
	for i, a := range full {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, strings.Join(quoted, " "), nil)
}

// moduleNosh implements Ansible's `nosh` (community.general) module:
// controls a service's running/enabled state under the nosh
// process-supervision suite's own `system-control` command — a direct
// port (no CLI substitution needed: real nosh.py already shells out to
// `system-control` itself via module.run_command(), exactly as this
// port does via noshRun/conn.Exec).
//
// Args: name (string, required); state (started|stopped|reset|
// restarted|reloaded, optional); enabled (bool, optional, mutually
// exclusive with preset); preset (bool, optional, mutually exclusive
// with enabled — only has an effect when true); user (bool, default
// false) — `--user` talks to the calling user's own service manager
// instead of the system-wide one; at least one of state/enabled/preset
// is effectively required for this module to do anything, but — unlike
// real nosh.py, which has no required_one_of/required_if at all for
// these — this port does not error when none are given: it simply
// gathers and returns the service's current facts, matching real
// nosh.py's own actual runtime behavior (its argument_spec has no such
// constraint despite the EXAMPLES showing a facts-only invocation with
// no state/enabled/preset at all).
//
// name is resolved to its own service_path via `system-control find
// <name>` first; a service system-control cannot find is
// Result{Failed:true} (matching real nosh.py's own fail_if_missing
// call), not a Go error — a well-formed request the target simply
// doesn't have. Enabled/preset state is applied before running state
// (state=reset consults the FINAL enabled state to decide start vs
// stop), matching real nosh.py's own handle_enabled-before-handle_state
// order exactly, including its own "preset changes enabled" chaining:
// applying preset flips both result.preset and result.enabled (if
// preset actually needed applying), and applying enabled again flips
// preset — see handle_enabled's own doc comment on noshHandleEnabled.
// state=started/stopped are idempotent against the service's own
// current DaemontoolsEncoreState (matching real service_is_running());
// state=restarted/reloaded still act idempotently in ONE direction
// only — matching real nosh.py's own quirk verbatim: both merely
// *start* the service if it isn't loaded/running yet (never actually
// restart/reload anything in that case), and only bounce
// (condrestart/hangup) an already-running one; state=reset starts or
// stops the service to match its own enabled flag, matching real
// nosh.py's own handle_state exactly. A service that isn't loaded at
// all has no meaningful status (`status` is nil, `state` may be nil
// too), matching real nosh.py's own None-valued result fields for this
// case.
//
// Extra: name, service_path, enabled, preset, user, status (a
// map[string]any decoded from `system-control show-json`'s own single
// entry for service_path, or nil if not loaded), and state (the
// resulting run state, or nil if the state argument wasn't used and the
// service isn't loaded) — matching real nosh.py's own RETURN block
// field-for-field.
func moduleNosh(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	user := argBool(args, "user", false)
	stateArg := argString(args, "state", "")
	if stateArg != "" {
		switch stateArg {
		case "started", "stopped", "reset", "restarted", "reloaded":
		default:
			return Result{}, errArg("nosh: state must be one of started, stopped, reset, restarted, reloaded, got %q", stateArg)
		}
	}
	_, enabledSet := args["enabled"]
	enabledWant := argBool(args, "enabled", false)
	preset := argBool(args, "preset", false)
	if enabledSet && preset {
		return Result{}, errArg("nosh: enabled and preset are mutually exclusive")
	}

	servicePath, err := noshServicePath(ctx, conn, user, name)
	if err != nil {
		return Result{}, err
	}
	if servicePath == "" {
		return Fail(fmt.Sprintf("could not find service %q", name)), nil
	}

	enabled, err := noshIsEnabled(ctx, conn, user, servicePath)
	if err != nil {
		return Result{}, err
	}
	presetEnabled, err := noshIsPresetEnabled(ctx, conn, user, servicePath)
	if err != nil {
		return Result{}, err
	}
	currentPreset := enabled == presetEnabled

	changed := false
	if enabledSet || preset {
		c, newEnabled, newPreset, failMsg, err := noshHandleEnabled(ctx, conn, user, servicePath, enabled, currentPreset, enabledSet, enabledWant, preset)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		changed = changed || c
		enabled = newEnabled
		currentPreset = newPreset
	}

	var resultState string
	var status map[string]any
	if stateArg != "" {
		c, rs, failMsg, err := noshHandleState(ctx, conn, user, servicePath, stateArg, enabled)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		changed = changed || c
		resultState = rs
	}

	loaded, err := noshIsLoaded(ctx, conn, user, servicePath)
	if err != nil {
		return Result{}, err
	}
	if loaded {
		status, err = noshShowJSON(ctx, conn, user, servicePath)
		if err != nil {
			return Result{}, err
		}
	}

	res := Result{Changed: changed}
	res = res.WithExtra("name", name)
	res = res.WithExtra("service_path", servicePath)
	res = res.WithExtra("enabled", enabled)
	res = res.WithExtra("preset", currentPreset)
	res = res.WithExtra("user", user)
	res = res.WithExtra("status", status)
	if stateArg != "" {
		res = res.WithExtra("state", resultState)
	}
	return res, nil
}

func noshServicePath(ctx context.Context, conn remoteexec.Connection, user bool, name string) (string, error) {
	res, err := noshRun(ctx, conn, user, []string{"find", name})
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	return strings.TrimSpace(res.Stdout), nil
}

func noshIsEnabled(ctx context.Context, conn remoteexec.Connection, user bool, servicePath string) (bool, error) {
	res, err := noshRun(ctx, conn, user, []string{"is-enabled", servicePath})
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

func noshIsPresetEnabled(ctx context.Context, conn remoteexec.Connection, user bool, servicePath string) (bool, error) {
	res, err := noshRun(ctx, conn, user, []string{"preset", "--dry-run", servicePath})
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.TrimSpace(res.Stdout), "enable"), nil
}

func noshIsLoaded(ctx context.Context, conn remoteexec.Connection, user bool, servicePath string) (bool, error) {
	res, err := noshRun(ctx, conn, user, []string{"is-loaded", servicePath})
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

func noshShowJSON(ctx context.Context, conn remoteexec.Connection, user bool, servicePath string) (map[string]any, error) {
	res, err := noshRun(ctx, conn, user, []string{"show-json", servicePath})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(res.Stderr) != "" {
		return nil, fmt.Errorf("nosh: system-control show-json %s: %s", servicePath, strings.TrimSpace(res.Stderr))
	}
	var full map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &full); err != nil {
		return nil, fmt.Errorf("nosh: parsing show-json output: %w", err)
	}
	raw, ok := full[servicePath]
	if !ok {
		return nil, nil
	}
	var status map[string]any
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil, fmt.Errorf("nosh: parsing show-json status for %s: %w", servicePath, err)
	}
	return status, nil
}

func noshIsRunning(status map[string]any) bool {
	if status == nil {
		return false
	}
	s, _ := status["DaemontoolsEncoreState"].(string)
	switch s {
	case "starting", "started", "running":
		return true
	}
	return false
}

// noshHandleEnabled applies preset (if requested) then enabled (if
// requested), matching real nosh.py's own handle_enabled() action order
// and its own preset-flips-enabled / enabled-flips-preset bookkeeping.
func noshHandleEnabled(ctx context.Context, conn remoteexec.Connection, user bool, servicePath string, enabled, preset bool, enabledSet, enabledWant, presetWant bool) (changed bool, newEnabled, newPreset bool, failMsg string, err error) {
	newEnabled, newPreset = enabled, preset
	if presetWant {
		if preset != presetWant {
			res, err := noshRun(ctx, conn, user, []string{"preset", servicePath})
			if err != nil {
				return false, enabled, preset, "", err
			}
			if res.RC != 0 {
				return false, enabled, preset, fmt.Sprintf("Unable to preset service %s: %s%s", servicePath, res.Stdout, res.Stderr), nil
			}
			changed = true
			newPreset = !preset
			newEnabled = !enabled
		}
	}
	if enabledSet {
		if newEnabled != enabledWant {
			action := "disable"
			if enabledWant {
				action = "enable"
			}
			res, err := noshRun(ctx, conn, user, []string{action, servicePath})
			if err != nil {
				return changed, enabled, preset, "", err
			}
			if res.RC != 0 {
				return changed, enabled, preset, fmt.Sprintf("Unable to %s service %s: %s%s", action, servicePath, res.Stdout, res.Stderr), nil
			}
			changed = true
			newEnabled = !newEnabled
			newPreset = !newPreset
		}
	}
	return changed, newEnabled, newPreset, "", nil
}

// noshHandleState drives servicePath toward state, matching real
// nosh.py's own handle_state() exactly — including its own documented
// quirk that restarted/reloaded only ever *start* an unloaded service
// rather than genuinely restarting/reloading it.
func noshHandleState(ctx context.Context, conn remoteexec.Connection, user bool, servicePath, state string, enabled bool) (changed bool, resultState, failMsg string, err error) {
	resultState = state
	loaded, err := noshIsLoaded(ctx, conn, user, servicePath)
	if err != nil {
		return false, "", "", err
	}

	var action string
	if !loaded {
		switch state {
		case "started", "restarted", "reloaded":
			action = "start"
			resultState = "started"
		case "reset":
			if enabled {
				action = "start"
				resultState = "started"
			} else {
				resultState = ""
			}
		default:
			resultState = ""
		}
	} else {
		status, err := noshShowJSON(ctx, conn, user, servicePath)
		if err != nil {
			return false, "", "", err
		}
		running := noshIsRunning(status)
		switch state {
		case "started":
			if !running {
				action = "start"
			}
		case "stopped":
			if running {
				action = "stop"
			}
		case "reset":
			if enabled != running {
				if running {
					action = "stop"
					resultState = "stopped"
				} else {
					action = "start"
					resultState = "started"
				}
			}
		case "restarted":
			if !running {
				action = "start"
				resultState = "started"
			} else {
				action = "condrestart"
			}
		case "reloaded":
			if !running {
				action = "start"
				resultState = "started"
			} else {
				action = "hangup"
			}
		}
	}

	if action == "" {
		return false, resultState, "", nil
	}
	res, err := noshRun(ctx, conn, user, []string{action, servicePath})
	if err != nil {
		return false, "", "", err
	}
	if res.RC != 0 {
		return false, "", fmt.Sprintf("Unable to %s service %s: %s", action, servicePath, strings.TrimSpace(res.Stderr)), nil
	}
	return true, resultState, "", nil
}
