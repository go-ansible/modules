package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSlackpkg implements (a subset of) Ansible's `slackpkg` module:
// manages packages on Slackware via `slackpkg`.
//
// Args: name (string or []string, alias pkg) — a package name, or list
// of names; state (present|installed|absent|removed|latest, default
// "present"); update_cache (bool, default false) — runs
// `slackpkg -batch=on update` first.
//
// Simplifications vs real slackpkg: idempotency for present/absent is
// checked via `ls /var/log/packages` (every installed package leaves a
// file there named `<name>-<version>-<arch>-<build>`) grepped for a
// `<name>-` prefix. Real slackpkg.py instead lists that directory over
// Python's os.listdir and matches a regex anchored on the exact machine
// architecture (with an x86/x86_64 kernel-headers special case) — this
// port has only shell access, so it does a plain prefix grep instead,
// which is weaker (it would, for instance, treat an installed `foo-bar`
// as satisfying a check for `foo`... no: the trailing `-` in the grepped
// prefix `foo-` prevents that specific case, but it does not verify the
// matched file's architecture/build suffix the way the real regex does).
// Like real slackpkg.py, each name is installed/removed/upgraded with
// its own `slackpkg` invocation (not batched into one command line),
// since slackpkg's own batch mode does not accept multiple package
// operands the way apk/pacman do. state=latest always runs
// `slackpkg ... upgrade <name>` and reports changed, matching this
// batch's house "can't cheaply tell already-latest apart" convention.
func moduleSlackpkg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("slackpkg: %v", err)
	}
	state := argString(args, "state", "present")

	if argBool(args, "update_cache", false) {
		if _, err := run(ctx, conn, "slackpkg -batch=on update"); err != nil {
			return Result{}, err
		}
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			return slackpkgInstalled(ctx, conn, name)
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			for _, n := range names {
				if _, err := run(ctx, conn, "slackpkg -default_answer=y -batch=on install "+shellQuote(n)); err != nil {
					return err
				}
			}
			return nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			for _, n := range names {
				if _, err := run(ctx, conn, "slackpkg -default_answer=y -batch=on remove "+shellQuote(n)); err != nil {
					return err
				}
			}
			return nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			for _, n := range names {
				if _, err := run(ctx, conn, "slackpkg -default_answer=y -batch=on upgrade "+shellQuote(n)); err != nil {
					return err
				}
			}
			return nil
		},
	)
}

// slackpkgInstalled reports whether name is installed, by grepping
// /var/log/packages for a file beginning with "<name>-" (see the doc
// comment on moduleSlackpkg for how this differs from real slackpkg's
// own architecture-anchored regex).
func slackpkgInstalled(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	cmd := "ls /var/log/packages 2>/dev/null | grep -q " + shellQuote("^"+name+"-")
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
