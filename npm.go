package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleNpm implements (a subset of) Ansible's `npm` module: manages
// Node.js packages via `npm`.
//
// Args: name (string, optional — when omitted, installs from
// package.json in path/cwd, matching `npm install` with no args);
// path (string) — directory to run npm in (ignored when global=true);
// global (bool, default false); version (string, optional); state
// (present|absent|latest, default "present"); ci (bool, default
// false) — runs `npm ci` instead of `npm install` (only meaningful
// with no name, matching real npm's own semantics); production (bool,
// default false) — adds `--production`.
//
// Simplifications vs real npm: no `executable`, `force`,
// `ignore_scripts`, `no_bin_links`, `no_optional`, `registry`,
// `unsafe_perm` support. Idempotency for a named package is checked
// via `npm list [-g] [--prefix path] <name>` (exit 0 iff installed at
// any version — a version-pinned request does not re-check the
// installed version against the requested one, so `state: present`
// with a `version` that differs from what's already installed is a
// no-op; real npm has the same limitation for its own `present`
// state, only distinguishing versions for `state: latest`).
// state=latest always runs `npm install -g/--prefix <name>@latest` (or
// plain `npm update` with no name) and reports changed, matching this
// batch's house "can't cheaply tell already-latest apart" convention.
func moduleNpm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", "")
	path := argString(args, "path", "")
	global := argBool(args, "global", false)
	version := argString(args, "version", "")
	state := argString(args, "state", "present")
	ci := argBool(args, "ci", false)
	production := argBool(args, "production", false)

	locFlag := ""
	if global {
		locFlag = " -g"
	} else if path != "" {
		locFlag = " --prefix " + shellQuote(path)
	}
	prodFlag := ""
	if production {
		prodFlag = " --production"
	}

	if name == "" {
		verb := "install"
		if ci {
			verb = "ci"
		}
		if state == "latest" {
			verb = "update"
		}
		cmd := "npm " + verb + locFlag + prodFlag
		if path != "" && !global {
			cmd = "cd " + shellQuote(path) + " && " + cmd
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("npm " + verb), nil
	}

	pkgSpec := name
	if version != "" {
		pkgSpec = name + "@" + version
	}

	switch state {
	case "absent":
		installed, err := npmInstalled(ctx, conn, locFlag, name)
		if err != nil {
			return Result{}, err
		}
		if !installed {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "npm uninstall"+locFlag+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	case "latest":
		spec := name + "@latest"
		if _, err := run(ctx, conn, "npm install"+locFlag+prodFlag+" "+shellQuote(spec)); err != nil {
			return Result{}, err
		}
		return Changed(name + " updated"), nil

	default: // "present"
		installed, err := npmInstalled(ctx, conn, locFlag, name)
		if err != nil {
			return Result{}, err
		}
		if installed {
			return Ok(name + " already installed"), nil
		}
		if _, err := run(ctx, conn, "npm install"+locFlag+prodFlag+" "+shellQuote(pkgSpec)); err != nil {
			return Result{}, err
		}
		return Changed(name + " installed"), nil
	}
}

func npmInstalled(ctx context.Context, conn remoteexec.Connection, locFlag, name string) (bool, error) {
	res, err := conn.Exec(ctx, "npm list"+locFlag+" "+shellQuote(name)+" >/dev/null 2>&1", nil)
	if err != nil {
		return false, fmt.Errorf("checking npm package %s: %w", name, err)
	}
	return res.RC == 0, nil
}
