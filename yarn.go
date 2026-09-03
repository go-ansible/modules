package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleYarn implements (a subset of) Ansible's `yarn` module: manages
// Node.js packages via Yarn Classic.
//
// Args: name (string, optional — when omitted, installs all packages
// from package.json); path (string) — directory to run yarn in; global
// (bool, default false); version (string, optional, semver); state
// (present|absent|latest, default "present"); production (bool,
// default false).
//
// Simplifications vs real yarn: no `executable`, `ignore_scripts`, or
// `registry` support. Idempotency for a named package is checked via
// `yarn [global] list [--cwd path] --pattern <name>` grepping for
// `"<name>@"`, matching this batch's house best-effort text-check
// convention (real yarn parses this same output more strictly, but a
// substring check is sufficient for the common case of a package name
// with no special regex characters). state=latest always runs `yarn
// [global] add <name>@latest` and reports changed, matching apt.go's
// "can't cheaply tell already-latest apart" convention.
func moduleYarn(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", "")
	path := argString(args, "path", "")
	global := argBool(args, "global", false)
	version := argString(args, "version", "")
	state := argString(args, "state", "present")
	production := argBool(args, "production", false)

	scope := ""
	if global {
		scope = "global "
	}
	cwdFlag := ""
	if path != "" && !global {
		cwdFlag = " --cwd " + shellQuote(path)
	}
	prodFlag := ""
	if production {
		prodFlag = " --production"
	}

	if name == "" {
		cmd := "yarn install" + cwdFlag + prodFlag
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("yarn install"), nil
	}

	pkgSpec := name
	if version != "" {
		pkgSpec = name + "@" + version
	}

	switch state {
	case "absent":
		installed, err := yarnInstalled(ctx, conn, scope, cwdFlag, name)
		if err != nil {
			return Result{}, err
		}
		if !installed {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "yarn "+scope+"remove"+cwdFlag+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	case "latest":
		if _, err := run(ctx, conn, "yarn "+scope+"add"+cwdFlag+prodFlag+" "+shellQuote(name+"@latest")); err != nil {
			return Result{}, err
		}
		return Changed(name + " updated"), nil

	default: // "present"
		installed, err := yarnInstalled(ctx, conn, scope, cwdFlag, name)
		if err != nil {
			return Result{}, err
		}
		if installed {
			return Ok(name + " already installed"), nil
		}
		if _, err := run(ctx, conn, "yarn "+scope+"add"+cwdFlag+prodFlag+" "+shellQuote(pkgSpec)); err != nil {
			return Result{}, err
		}
		return Changed(name + " installed"), nil
	}
}

func yarnInstalled(ctx context.Context, conn remoteexec.Connection, scope, cwdFlag, name string) (bool, error) {
	res, err := conn.Exec(ctx, "yarn "+scope+"list"+cwdFlag+" --pattern "+shellQuote(name)+" 2>/dev/null | grep -qF "+shellQuote(name+"@"), nil)
	if err != nil {
		return false, fmt.Errorf("checking yarn package %s: %w", name, err)
	}
	return res.RC == 0, nil
}
