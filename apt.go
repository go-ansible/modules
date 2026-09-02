package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleApt implements (a subset of) Ansible's `apt` module for
// Debian/Ubuntu package management.
//
// Args: name (string or []string, required); state (present|installed|
// absent|latest, default "present"); update_cache (bool, default
// false) — run `apt-get update` first.
func moduleApt(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		if s, err := requireString(args, "name"); err == nil {
			names = []string{s}
		} else {
			return Result{}, errArg("apt: missing required argument: name")
		}
	}
	state := argString(args, "state", "present")

	if argBool(args, "update_cache", false) {
		if _, err := run(ctx, conn, "DEBIAN_FRONTEND=noninteractive apt-get update -q"); err != nil {
			return Result{}, err
		}
	}

	changed := false
	switch state {
	case "absent":
		for _, name := range names {
			installed, err := dpkgInstalled(ctx, conn, name)
			if err != nil {
				return Result{}, err
			}
			if !installed {
				continue
			}
			if _, err := run(ctx, conn, "DEBIAN_FRONTEND=noninteractive apt-get remove -y -q "+shellQuote(name)); err != nil {
				return Result{}, err
			}
			changed = true
		}

	case "latest":
		cmd := "DEBIAN_FRONTEND=noninteractive apt-get install -y -q --only-upgrade " + quoteAll(names)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true // apt-get's own idempotent no-op still exits 0; we can't cheaply distinguish "already latest" without parsing output

	default: // "present" / "installed"
		var toInstall []string
		for _, name := range names {
			installed, err := dpkgInstalled(ctx, conn, name)
			if err != nil {
				return Result{}, err
			}
			if !installed {
				toInstall = append(toInstall, name)
			}
		}
		if len(toInstall) == 0 {
			return Ok("all packages already installed"), nil
		}
		cmd := "DEBIAN_FRONTEND=noninteractive apt-get install -y -q " + quoteAll(toInstall)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed(strings.Join(names, ", ")), nil
	}
	return Ok("unchanged"), nil
}

func dpkgInstalled(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := conn.Exec(ctx, "dpkg -s "+shellQuote(name)+" 2>/dev/null | grep -q '^Status:.*installed'", nil)
	if err != nil {
		return false, fmt.Errorf("checking package %s: %w", name, err)
	}
	return res.RC == 0, nil
}

func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = shellQuote(n)
	}
	return strings.Join(quoted, " ")
}
