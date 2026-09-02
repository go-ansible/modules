package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDnf implements (a subset of) Ansible's `dnf` module for
// RPM-based package management via the classic `dnf` CLI.
//
// Args: name (string or []string, required); state (present|latest|
// absent, default "present").
//
// This is a thin wrapper around dnfLike (see below), shared with
// moduleDnf5.
func moduleDnf(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return dnfLike(ctx, conn, args, "dnf")
}

// dnfLike implements the install/remove/upgrade logic shared by
// moduleDnf and moduleDnf5, against binary ("dnf" or "dnf5"), checking
// package presence via `rpm -q` (present on both families' targets,
// since dnf/dnf5 both use the RPM database) before acting — the same
// house pattern as apt.go/pip.go's package-manager modules.
func dnfLike(ctx context.Context, conn remoteexec.Connection, args map[string]any, binary string) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		if s, err := requireString(args, "name"); err == nil {
			names = []string{s}
		} else {
			return Result{}, errArg("%s: missing required argument: name", binary)
		}
	}
	state := argString(args, "state", "present")

	changed := false
	switch state {
	case "absent":
		for _, name := range names {
			installed, err := rpmInstalled(ctx, conn, name)
			if err != nil {
				return Result{}, err
			}
			if !installed {
				continue
			}
			if _, err := run(ctx, conn, binary+" remove -y "+shellQuote(name)); err != nil {
				return Result{}, err
			}
			changed = true
		}

	case "latest":
		cmd := binary + " update -y " + quoteAll(names)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true // a no-op update still exits 0; like apt's "latest", we can't cheaply tell "already latest" apart without parsing output

	default: // "present"
		var toInstall []string
		for _, name := range names {
			installed, err := rpmInstalled(ctx, conn, name)
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
		cmd := binary + " install -y " + quoteAll(toInstall)
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

func rpmInstalled(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := conn.Exec(ctx, "rpm -q "+shellQuote(name)+" >/dev/null 2>&1", nil)
	if err != nil {
		return false, fmt.Errorf("checking package %s: %w", name, err)
	}
	return res.RC == 0, nil
}
