package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePnpm implements (a subset of) Ansible's `pnpm` module: manages
// Node.js packages via `pnpm`.
//
// Args: name (string, optional — when omitted, installs from
// package.json); path (string) — directory to run pnpm in; global
// (bool, default false); version (string, optional); state (present|
// absent|latest, default "present"); production (bool, default
// false).
//
// Simplifications vs real pnpm: no `alias`, `dev`, `executable`,
// `ignore_scripts`, or `no_optional`/`optional` support. Idempotency
// for a named package is checked via `pnpm list [-g] [-C path]
// <name>` grepping for `<name>` in the output, matching this batch's
// house best-effort text-check convention. state=latest always runs
// `pnpm add [-g] <name>@latest` and reports changed, matching apt.go's
// "can't cheaply tell already-latest apart" convention.
func modulePnpm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", "")
	path := argString(args, "path", "")
	global := argBool(args, "global", false)
	version := argString(args, "version", "")
	state := argString(args, "state", "present")
	production := argBool(args, "production", false)

	locFlag := ""
	if global {
		locFlag = " -g"
	} else if path != "" {
		locFlag = " -C " + shellQuote(path)
	}
	prodFlag := ""
	if production {
		prodFlag = " --prod"
	}

	if name == "" {
		cmd := "pnpm install" + locFlag + prodFlag
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("pnpm install"), nil
	}

	pkgSpec := name
	if version != "" {
		pkgSpec = name + "@" + version
	}

	switch state {
	case "absent":
		installed, err := pnpmInstalled(ctx, conn, locFlag, name)
		if err != nil {
			return Result{}, err
		}
		if !installed {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "pnpm remove"+locFlag+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	case "latest":
		if _, err := run(ctx, conn, "pnpm add"+locFlag+prodFlag+" "+shellQuote(name+"@latest")); err != nil {
			return Result{}, err
		}
		return Changed(name + " updated"), nil

	default: // "present"
		installed, err := pnpmInstalled(ctx, conn, locFlag, name)
		if err != nil {
			return Result{}, err
		}
		if installed {
			return Ok(name + " already installed"), nil
		}
		if _, err := run(ctx, conn, "pnpm add"+locFlag+prodFlag+" "+shellQuote(pkgSpec)); err != nil {
			return Result{}, err
		}
		return Changed(name + " installed"), nil
	}
}

func pnpmInstalled(ctx context.Context, conn remoteexec.Connection, locFlag, name string) (bool, error) {
	res, err := conn.Exec(ctx, "pnpm list"+locFlag+" "+shellQuote(name)+" 2>/dev/null | grep -qF "+shellQuote(name), nil)
	if err != nil {
		return false, fmt.Errorf("checking pnpm package %s: %w", name, err)
	}
	return res.RC == 0, nil
}
