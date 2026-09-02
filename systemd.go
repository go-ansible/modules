package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSystemd implements (a subset of) Ansible's `systemd` module.
//
// Args: name (string, required); state (started|stopped|restarted|
// reloaded — optional, no default: when unset only `enabled` is
// applied); enabled (bool, optional).
func moduleSystemd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "")

	changed := false

	if state != "" {
		active, err := unitIsActive(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		var verb string
		switch state {
		case "started":
			if active {
				verb = ""
			} else {
				verb = "start"
			}
		case "stopped":
			if !active {
				verb = ""
			} else {
				verb = "stop"
			}
		case "restarted":
			verb = "restart"
		case "reloaded":
			verb = "reload"
		default:
			return Result{}, errArg("systemd: unknown state %q", state)
		}
		if verb != "" {
			if _, err := run(ctx, conn, "systemctl "+verb+" "+shellQuote(name)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if _, ok := args["enabled"]; ok {
		wantEnabled := argBool(args, "enabled", false)
		isEnabled, err := unitIsEnabled(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if wantEnabled != isEnabled {
			verb := "disable"
			if wantEnabled {
				verb = "enable"
			}
			if _, err := run(ctx, conn, "systemctl "+verb+" "+shellQuote(name)); err != nil {
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

func unitIsActive(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := conn.Exec(ctx, "systemctl is-active "+shellQuote(name), nil)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", name, err)
	}
	return res.RC == 0, nil
}

func unitIsEnabled(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := conn.Exec(ctx, "systemctl is-enabled "+shellQuote(name), nil)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", name, err)
	}
	return res.RC == 0, nil
}
