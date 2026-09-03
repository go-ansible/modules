package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHomebrewServices implements (a subset of) Ansible's
// `homebrew_services` module: starts, stops, or restarts a Homebrew
// package's background service via `brew services`.
//
// Args: name (string, required); state (present|absent|restarted,
// default "present").
//
// Simplifications vs real homebrew_services: no `path` support (always
// uses `brew` from PATH). Idempotency is checked via `brew services
// list`, grepping for a "<name> started" line; state=restarted always
// runs `brew services restart` and reports changed, matching real
// homebrew_services's own behavior (a restart is inherently an action,
// not a state comparison).
func moduleHomebrewServices(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")

	switch state {
	case "present":
		running, err := homebrewServiceRunning(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if running {
			return Ok(name + " already started"), nil
		}
		if _, err := run(ctx, conn, "brew services start "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " started"), nil

	case "absent":
		running, err := homebrewServiceRunning(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if !running {
			return Ok(name + " already stopped"), nil
		}
		if _, err := run(ctx, conn, "brew services stop "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " stopped"), nil

	case "restarted":
		if _, err := run(ctx, conn, "brew services restart "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " restarted"), nil

	default:
		return Result{}, errArg("homebrew_services: state must be present, absent, or restarted, got %q", state)
	}
}

func homebrewServiceRunning(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "brew services list 2>/dev/null | grep -qE "+shellQuote("^"+name+" +started"))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
