package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOpenbsdPkg implements (a subset of) Ansible's `openbsd_pkg`
// module: manages packages on OpenBSD via `pkg_add`/`pkg_delete`/
// `pkg_info`.
//
// Args: name (string or []string) — a package name, or list of names; a
// single name of "*" is only meaningful with state=latest and runs a
// whole-system `pkg_add -um` (matching real openbsd_pkg's own
// name=="*" system-update shorthand); state (present|installed|absent|
// removed|latest, default "present"); clean (bool, default false) —
// adds pkg_add/pkg_delete's `c` flag, deleting `@extra`-annotated extra
// configuration files on removal/upgrade; quick (bool, default false) —
// adds the `q` flag, skipping checksum verification; autoremove (bool,
// default false) — after the main operation, also runs
// `pkg_delete -Ia[c][q]` to remove now-unneeded automatically-installed
// packages, always reported changed since (like a no-op `pkg_delete -a`
// run) this port can't cheaply tell "there was nothing to remove" apart
// from stdout parsing.
//
// Simplifications vs real openbsd_pkg: no `build` (building from the
// ports tree instead of a binary package), `ports_dir`, or `snapshot`
// support. Real openbsd_pkg parses OpenBSD's packages-specs(7) syntax
// (`name--flavor`, `name%branch`) out of each name to separate the
// "stem" from flavor/branch qualifiers for its own idempotency queries;
// this port passes each name through to pkg_add/pkg_delete/pkg_info
// as-is without that parsing, so a flavor/branch-qualified name's
// idempotency check is only as accurate as `pkg_info -e <name>` itself
// is for that syntax. Idempotency for present/absent is checked via
// `pkg_info -e <name>` (exit 0 iff installed, an exact existence test
// analogous to FreeBSD pkgng's `pkg info -e`); state=latest always runs
// `pkg_add -um <name>` and reports changed, matching this batch's house
// "can't cheaply tell already-latest apart" convention.
func moduleOpenbsdPkg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("openbsd_pkg: %v", err)
	}
	state := argString(args, "state", "present")
	clean := argBool(args, "clean", false)
	quick := argBool(args, "quick", false)

	flags := func(base string) string {
		if clean {
			base += "c"
		}
		if quick {
			base += "q"
		}
		return base
	}

	var res Result
	switch {
	case len(names) == 1 && names[0] == "*" && state == "latest":
		if _, err := run(ctx, conn, "pkg_add "+flags("-um")); err != nil {
			return Result{}, err
		}
		res = Changed("system upgraded")
	case len(names) == 1 && names[0] == "*":
		// "*" is only meaningful with state=latest (a whole-system
		// update) or on its own with autoremove — for any other state
		// it's a no-op, matching real openbsd_pkg's own handling of a
		// bare "*" as a system-wide token rather than a literal package
		// name.
		res = Ok("name=* is a no-op for state=" + state)
	default:
		res, err = pkgManagerLoop(ctx, conn, names, state,
			func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
				r, err := conn.Exec(ctx, "pkg_info -e "+shellQuote(name)+" >/dev/null 2>&1", nil)
				if err != nil {
					return false, fmt.Errorf("checking openbsd_pkg package %s: %w", name, err)
				}
				return r.RC == 0, nil
			},
			func(ctx context.Context, conn remoteexec.Connection, names []string) error {
				_, err := run(ctx, conn, "pkg_add -Im "+quoteAll(names))
				return err
			},
			func(ctx context.Context, conn remoteexec.Connection, names []string) error {
				_, err := run(ctx, conn, "pkg_delete "+flags("-I")+" "+quoteAll(names))
				return err
			},
			func(ctx context.Context, conn remoteexec.Connection, names []string) error {
				_, err := run(ctx, conn, "pkg_add "+flags("-um")+" "+quoteAll(names))
				return err
			},
		)
		if err != nil {
			return Result{}, err
		}
	}

	if argBool(args, "autoremove", false) {
		if _, err := run(ctx, conn, "pkg_delete "+flags("-Ia")); err != nil {
			return Result{}, err
		}
		res.Changed = true
	}

	return res, nil
}
