package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAwall implements Ansible's `awall` module (community.general):
// enables, disables, or activates Alpine Wall (`awall`) firewall
// policies, via `awall enable`/`awall disable`/`awall activate`/`awall
// list`.
//
// Args: name ([]string, optional) — one or more policy names; state
// (enabled|disabled, default "enabled") — applies to `name`; activate
// (bool, default false) — also runs `awall activate --force`
// afterward (or, if `name` is empty, is the ONLY action taken); at
// least one of name/activate is required, matching real awall.py's own
// `required_one_of`.
//
// A policy's current enabled state is read from `awall list`'s own
// output, matched by `^<name>\s+enabled` per real awall.py's regex —
// this port instead splits each line into fields and compares
// field[0]==name / field[1]=="enabled", a simplified approximation of
// the same "name, then its enabled/disabled state" listing shape.
// Enabling/disabling is skipped entirely for a policy already in the
// desired state (batched into a single `awall enable name1 name2 ...`/
// `awall disable ...` for whichever policies still need it). Per real
// awall.py's own documented note, activate=true always reports
// Changed for the activation step itself (activation has no
// "already applied" concept this port — or real awall.py — checks
// for).
func moduleAwall(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	state := argString(args, "state", "enabled")
	activate := argBool(args, "activate", false)
	if len(names) == 0 && !activate {
		return Result{}, errArg("awall: at least one of name or activate is required")
	}

	if len(names) > 0 {
		switch state {
		case "enabled":
			return awallSetPolicy(ctx, conn, names, true, activate)
		case "disabled":
			return awallSetPolicy(ctx, conn, names, false, activate)
		default:
			return Result{}, errArg("awall: state must be enabled or disabled, got %q", state)
		}
	}

	if _, err := run(ctx, conn, "awall activate --force"); err != nil {
		return Result{}, err
	}
	return Changed("activated awall rules"), nil
}

func awallListEnabled(ctx context.Context, conn remoteexec.Connection) (map[string]bool, error) {
	out, err := run(ctx, conn, "awall list")
	if err != nil {
		return nil, err
	}
	enabled := map[string]bool{}
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			enabled[fields[0]] = fields[1] == "enabled"
		}
	}
	return enabled, nil
}

func awallSetPolicy(ctx context.Context, conn remoteexec.Connection, names []string, wantEnabled, activate bool) (Result, error) {
	current, err := awallListEnabled(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	var toChange []string
	for _, n := range names {
		if current[n] != wantEnabled {
			toChange = append(toChange, n)
		}
	}
	verb := "enabled"
	if !wantEnabled {
		verb = "disabled"
	}
	if len(toChange) == 0 {
		return Ok(fmt.Sprintf("policy(ies) already %s", verb)), nil
	}
	action := "enable"
	if !wantEnabled {
		action = "disable"
	}
	cmd := "awall " + action
	for _, n := range toChange {
		cmd += " " + shellQuote(n)
	}
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	if activate {
		if _, err := run(ctx, conn, "awall activate --force"); err != nil {
			return Result{}, err
		}
	}
	return Changed(fmt.Sprintf("%s awall policy(ies): %s", verb, strings.Join(toChange, " "))), nil
}
