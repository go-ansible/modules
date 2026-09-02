package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePip implements (a subset of) Ansible's `pip` module.
//
// Args: name (string or []string, required); state (present|absent,
// default "present"); executable (string, default "pip3").
func modulePip(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		if s, err := requireString(args, "name"); err == nil {
			names = []string{s}
		} else {
			return Result{}, errArg("pip: missing required argument: name")
		}
	}
	state := argString(args, "state", "present")
	pip := argString(args, "executable", "pip3")

	changed := false
	switch state {
	case "absent":
		for _, name := range names {
			installed, err := pipInstalled(ctx, conn, pip, name)
			if err != nil {
				return Result{}, err
			}
			if !installed {
				continue
			}
			if _, err := run(ctx, conn, pip+" uninstall -y "+shellQuote(pkgName(name))); err != nil {
				return Result{}, err
			}
			changed = true
		}

	default: // "present"
		var toInstall []string
		for _, name := range names {
			installed, err := pipInstalled(ctx, conn, pip, pkgName(name))
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
		if _, err := run(ctx, conn, pip+" install "+quoteAll(toInstall)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed(strings.Join(names, ", ")), nil
	}
	return Ok("unchanged"), nil
}

// pkgName strips a version specifier (e.g. "requests==2.31.0" ->
// "requests") for the existence check.
func pkgName(spec string) string {
	for _, sep := range []string{"==", ">=", "<=", "!=", ">", "<"} {
		if i := strings.Index(spec, sep); i >= 0 {
			return spec[:i]
		}
	}
	return spec
}

func pipInstalled(ctx context.Context, conn remoteexec.Connection, pip, name string) (bool, error) {
	res, err := conn.Exec(ctx, pip+" show "+shellQuote(name)+" >/dev/null 2>&1", nil)
	if err != nil {
		return false, fmt.Errorf("checking pip package %s: %w", name, err)
	}
	return res.RC == 0, nil
}
