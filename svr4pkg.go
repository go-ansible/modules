package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSvr4pkg implements (a subset of) Ansible's `svr4pkg` module:
// manages legacy SVR4 packages on Solaris via `pkgadd`/`pkgrm` — a
// very basic packaging system with no dependency resolution (unlike
// pkgutil.go's OpenCSW packages).
//
// Args: name (string, required); state (present|absent, required);
// src (string, required when state=present) — anything acceptable to
// `pkgadd -d`, e.g. a file, directory, or URL; proxy (string,
// optional) — `-x proxy`; response_file (string, optional) — `-r
// response_file`; zone (current|all, default "all") — "current" adds
// `-G` (install into the current zone only, rather than every zone);
// category (bool, default false) — treat name as a package CATEGORY
// rather than a single package (`pkgadd`/`pkgrm`'s own `-Y` flag).
//
// Presence is checked via `pkginfo -q [-c] name` (`-c` when
// category); state=present installs via `pkgadd -n [-G] -d src [-x
// proxy] [-r response_file] [-Y] name` when not already present;
// state=absent removes via `pkgrm -n [-Y] name` when present.
//
// Simplification vs real svr4pkg: real svr4pkg composes a temporary
// `-a <adminfile>` admin-answers file (mail=, instance=unique,
// partial=nocheck, ..., authentication=quit, ...) on the target before
// every pkgadd/pkgrm call, to fully suppress every interactive prompt
// including dependency/conflict/setuid checks (each set to nocheck).
// This port passes only pkgadd/pkgrm's own `-n` (non-interactive)
// flag instead of composing that admin file: `-n` alone already
// suppresses the same summary/confirmation prompt for the common
// case, but (unlike the admin file's explicit nocheck directives) does
// NOT override a dependency/conflict/setuid question — a package
// install/remove that would need one of those specifically answered
// still exits non-zero here exactly as it would with plain `-n`,
// whereas real svr4pkg's admin file would let it proceed
// automatically. check_mode is not modeled (see
// zfs_delegate_admin.go's own doc comment).
func moduleSvr4pkg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("svr4pkg: state must be present or absent, got %q", state)
	}
	category := argBool(args, "category", false)
	zone := argString(args, "zone", "all")
	if zone != "current" && zone != "all" {
		return Result{}, errArg("svr4pkg: zone must be current or all, got %q", zone)
	}

	present, err := svr4pkgInstalled(ctx, conn, name, category)
	if err != nil {
		return Result{}, err
	}

	if state == "present" {
		if present {
			return Ok(name + " already present"), nil
		}
		src, err := requireString(args, "src")
		if err != nil {
			return Result{}, errArg("svr4pkg: src is required when state=present")
		}
		cmd := "pkgadd -n"
		if zone == "current" {
			cmd += " -G"
		}
		cmd += " -d " + shellQuote(src)
		if proxy := argString(args, "proxy", ""); proxy != "" {
			cmd += " -x " + shellQuote(proxy)
		}
		if respFile := argString(args, "response_file", ""); respFile != "" {
			cmd += " -r " + shellQuote(respFile)
		}
		if category {
			cmd += " -Y"
		}
		cmd += " " + shellQuote(name)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(name + " installed"), nil
	}

	if !present {
		return Ok(name + " already absent"), nil
	}
	cmd := "pkgrm -n"
	if category {
		cmd += " -Y"
	}
	cmd += " " + shellQuote(name)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(name + " removed"), nil
}

func svr4pkgInstalled(ctx context.Context, conn remoteexec.Connection, name string, category bool) (bool, error) {
	cmd := "pkginfo -q"
	if category {
		cmd += " -c"
	}
	cmd += " " + shellQuote(name)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return false, fmt.Errorf("checking svr4pkg package %s: %w", name, err)
	}
	return res.RC == 0, nil
}
