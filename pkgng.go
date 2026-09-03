package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePkgng implements (a subset of) Ansible's `pkgng` module: manages
// binary packages on FreeBSD via `pkg` (pkgng, the package manager that
// replaced the legacy pkg_add/pkg_delete/pkg_info tools since FreeBSD
// 9.0+).
//
// Args: name (string or []string, alias pkg) — a package name, or list
// of names; a single name of "*" means "every installed package" and is
// only meaningful with state=latest (matching real pkgng: with
// name=="*", state=present/absent are no-ops, and state=latest runs a
// whole-system `pkg upgrade`); state (present|latest|absent, default
// "present"); cached (bool, default false) — skip the `pkg update`
// repository-catalogue refresh that otherwise runs before an
// install/latest operation.
//
// Simplifications vs real pkgng: no `annotation` (tagging installed
// packages with key/value metadata), `autoremove`, `chroot`/`jail`/
// `rootdir` (targeting an alternate root), `ignore_osver`, `pkgsite`, or
// `use_globs` (glob-vs-literal name matching — this port always passes
// names through literally) support. Idempotency for present/absent is
// checked via `pkg info -e <name>` (exit 0 iff installed), matching
// this batch's house pattern; state=latest always runs `pkg upgrade -y
// <name>` and reports changed, for the same "can't cheaply tell
// already-latest apart" reason as apt.go/apk.go/pacman.go's own latest
// branch.
func modulePkgng(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("pkgng: %v", err)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "latest" && state != "absent" {
		return Result{}, errArg("pkgng: state must be present, latest, or absent, got %q", state)
	}
	cached := argBool(args, "cached", false)

	if len(names) == 1 && names[0] == "*" {
		if state != "latest" {
			return Ok("name=* is a no-op for state=" + state), nil
		}
		if !cached {
			if _, err := run(ctx, conn, "pkg update"); err != nil {
				return Result{}, err
			}
		}
		if _, err := run(ctx, conn, "pkg upgrade -y"); err != nil {
			return Result{}, err
		}
		return Changed("system upgraded"), nil
	}

	if !cached && (state == "present" || state == "latest") {
		if _, err := run(ctx, conn, "pkg update"); err != nil {
			return Result{}, err
		}
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			res, err := conn.Exec(ctx, "pkg info -e "+shellQuote(name)+" >/dev/null 2>&1", nil)
			if err != nil {
				return false, fmt.Errorf("checking pkgng package %s: %w", name, err)
			}
			return res.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pkg install -y "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pkg delete -y "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pkg upgrade -y "+quoteAll(names))
			return err
		},
	)
}
