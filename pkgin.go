package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePkgin implements (a subset of) Ansible's `pkgin` module:
// manages packages via `pkgin`, the binary package manager for pkgsrc
// (NetBSD, SmartOS, and other pkgsrc-based systems).
//
// Args: name (string or []string, alias pkg) — a package name, or list
// of names; state (present|absent, default "present"); update_cache
// (bool, default false) — runs `pkgin -y update` first; upgrade (bool,
// default false) — runs `pkgin -y upgrade` (upgrade "main" packages);
// full_upgrade (bool, default false) — runs `pkgin -y full-upgrade`
// (upgrade every package); clean (bool, default false) — runs
// `pkgin -y clean` (clean the package cache); force (bool, default
// false) — adds pkgin's `-F` force-reinstall flag to every pkgin
// invocation this run makes, matching real pkgin.py's own
// format_pkgin_command helper, which applies -F unconditionally rather
// than only to specific subcommands. At least one of name,
// update_cache, upgrade, full_upgrade, or clean is required, matching
// real pkgin's own required_one_of.
//
// Simplifications vs real pkgin: idempotency for present/absent is
// checked via `pkgin list` (installed packages only) grepped for a
// `<name>-` prefix line. Real pkgin.py instead runs `pkgin search
// ^name$` and parses per-line state markers (”, '<', '=', '>' for
// not-installed/outdated/current/newer) after first probing whether the
// installed pkgin supports its `-p` (parsable, semicolon-delimited)
// output format — this port does neither: it can't distinguish
// "installed but outdated" from "installed and current" (both count as
// present, so state=present never reinstalls an outdated package; use
// state=absent+present, or upgrade/full_upgrade, to force a version
// change). update_cache/upgrade/full_upgrade/clean each always report
// changed (matching this batch's house "can't cheaply tell a no-op
// apart" convention for whole-repository/whole-system operations).
func modulePkgin(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	updateCache := argBool(args, "update_cache", false)
	upgrade := argBool(args, "upgrade", false)
	fullUpgrade := argBool(args, "full_upgrade", false)
	clean := argBool(args, "clean", false)
	if len(names) == 0 && !updateCache && !upgrade && !fullUpgrade && !clean {
		return Result{}, errArg("pkgin: at least one of name, update_cache, upgrade, full_upgrade, or clean is required")
	}
	force := argBool(args, "force", false)
	forceFlag := ""
	if force {
		forceFlag = " -F"
	}

	changed := false
	if updateCache {
		if _, err := run(ctx, conn, "pkgin -y"+forceFlag+" update"); err != nil {
			return Result{}, err
		}
		changed = true
	}
	if upgrade {
		if _, err := run(ctx, conn, "pkgin -y"+forceFlag+" upgrade"); err != nil {
			return Result{}, err
		}
		changed = true
	}
	if fullUpgrade {
		if _, err := run(ctx, conn, "pkgin -y"+forceFlag+" full-upgrade"); err != nil {
			return Result{}, err
		}
		changed = true
	}
	if clean {
		if _, err := run(ctx, conn, "pkgin -y"+forceFlag+" clean"); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if len(names) == 0 {
		if !changed {
			return Ok("nothing to do"), nil
		}
		return Changed("cache/upgrade/clean step(s) ran"), nil
	}

	state := argString(args, "state", "present")
	res, err := pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			r, err := conn.Exec(ctx, "pkgin list 2>/dev/null | grep -q "+shellQuote("^"+name+"-"), nil)
			if err != nil {
				return false, fmt.Errorf("checking pkgin package %s: %w", name, err)
			}
			return r.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pkgin -y"+forceFlag+" install "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pkgin -y"+forceFlag+" remove "+quoteAll(names))
			return err
		},
		nil, // real pkgin has no state=latest; absent+present is the way to force a version change.
	)
	if err != nil {
		return Result{}, err
	}
	if changed && !res.Changed {
		res.Changed = true
	}
	return res, nil
}
