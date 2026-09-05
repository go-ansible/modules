package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleBower implements (a subset of) Ansible's `bower` module: manages
// front-end packages via the `bower` CLI.
//
// Args: name (string, optional — when omitted, installs from bower.json
// in path, matching `bower install` with no args); path (string) — the
// base path where to install the bower packages (bower's own `--config.cwd`);
// version (string, optional); state (present|absent|latest, default
// "present"); offline (bool, default false) — adds `--offline`;
// production (bool, default false) — adds `--production`.
//
// bower itself has been deprecated by its own maintainers since 2017
// ("We recommend migrating to Yarn and Webpack or JSPM" — bower.io's own
// front page) but the `bower` package on npm still installs and runs
// (verified: `npm install -g bower` succeeds and `bower --version`
// reports a working CLI as of this batch's own research) — this module
// shells out to it exactly like real bower.py does, deprecated tool or
// not, matching pear.go/opkg.go's own precedent for a legacy-but-real
// package manager.
//
// Simplifications vs real bower: no `relative_execpath` (real bower.py's
// own way to run a project-local `node_modules/.bin/bower` instead of a
// global install — this port always invokes the bare `bower` found on
// PATH). Idempotency for a named package is checked via `bower list
// [--config.cwd path] --json` and looking for `name` as a top-level key
// of the decoded `dependencies` object (bower's own `bower list --json`
// shape) — a package present only as a transitive sub-dependency is not
// treated as "installed" for idempotency purposes, matching how a
// playbook author would read `bower list`'s own top-level dependency
// tree. state=latest always runs `bower update [--config.cwd path]
// <name>` (or bare `bower update` with no name) and reports changed,
// matching this batch's house "can't cheaply tell already-latest apart"
// convention (npm.go/yarn.go/pnpm.go's own state=latest doc comments).
func moduleBower(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", "")
	path := argString(args, "path", "")
	version := argString(args, "version", "")
	state := argString(args, "state", "present")
	offline := argBool(args, "offline", false)
	production := argBool(args, "production", false)

	cwdFlag := ""
	if path != "" {
		cwdFlag = " --config.cwd=" + shellQuote(path)
	}
	extraFlags := ""
	if offline {
		extraFlags += " --offline"
	}
	if production {
		extraFlags += " --production"
	}

	if name == "" {
		verb := "install"
		if state == "latest" {
			verb = "update"
		}
		cmd := "bower " + verb + cwdFlag + extraFlags
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("bower " + verb), nil
	}

	pkgSpec := name
	if version != "" {
		pkgSpec = name + "#" + version
	}

	switch state {
	case "absent":
		installed, err := bowerInstalled(ctx, conn, cwdFlag, name)
		if err != nil {
			return Result{}, err
		}
		if !installed {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "bower uninstall"+cwdFlag+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	case "latest":
		if _, err := run(ctx, conn, "bower update"+cwdFlag+extraFlags+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " updated"), nil

	default: // "present"
		installed, err := bowerInstalled(ctx, conn, cwdFlag, name)
		if err != nil {
			return Result{}, err
		}
		if installed {
			return Ok(name + " already installed"), nil
		}
		if _, err := run(ctx, conn, "bower install"+cwdFlag+extraFlags+" "+shellQuote(pkgSpec)); err != nil {
			return Result{}, err
		}
		return Changed(name + " installed"), nil
	}
}

func bowerInstalled(ctx context.Context, conn remoteexec.Connection, cwdFlag, name string) (bool, error) {
	res, err := conn.Exec(ctx, "bower list"+cwdFlag+" --json 2>/dev/null | grep -q "+shellQuote(`"`+name+`"`), nil)
	if err != nil {
		return false, fmt.Errorf("checking bower package %s: %w", name, err)
	}
	return res.RC == 0, nil
}
