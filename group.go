package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGroup implements Ansible's `group` module: ensures a group
// exists or is removed.
//
// Args: name (string, required); state (present|absent, default
// "present"); system (bool) — pass -r/-system to groupadd.
func moduleGroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")

	exists, err := groupExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "absent":
		if !exists {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "groupdel "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	default: // "present"
		if exists {
			return Ok(name + " already present"), nil
		}
		cmd := "groupadd"
		if argBool(args, "system", false) {
			cmd += " -r"
		}
		cmd += " " + shellQuote(name)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(name + " created"), nil
	}
}

func groupExists(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := conn.Exec(ctx, "getent group "+shellQuote(name), nil)
	if err != nil {
		return false, fmt.Errorf("checking group %s: %w", name, err)
	}
	return res.RC == 0, nil
}
