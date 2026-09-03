package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleApk implements (a subset of) Ansible's `apk` module: manages
// packages via Alpine Linux's `apk` package manager.
//
// Args: name (string or []string) — a package name, or list of names;
// state (present|installed|absent|removed|latest, default "present");
// update_cache (bool, default false) — runs `apk update` first.
//
// Simplifications vs real apk: no `available`, `no_cache`,
// `repository`, `upgrade` (whole-system upgrade), or `world` (custom
// world file) support. Idempotency for present/absent is checked via
// `apk info -e <name>` (exit 0 iff installed), matching this batch's
// house pattern from apt.go/dnf.go/pip.go. state=latest always runs
// `apk add -u <name>` and reports changed, since (like apt's own
// "latest" branch) a no-op upgrade still exits 0 and telling "already
// latest" apart would require parsing apk's own output.
func moduleApk(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("apk: %v", err)
	}
	state := argString(args, "state", "present")

	if argBool(args, "update_cache", false) {
		if _, err := run(ctx, conn, "apk update -q"); err != nil {
			return Result{}, err
		}
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			res, err := conn.Exec(ctx, "apk info -e "+shellQuote(name)+" >/dev/null 2>&1", nil)
			if err != nil {
				return false, fmt.Errorf("checking apk package %s: %w", name, err)
			}
			return res.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "apk add -q "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "apk del -q "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "apk add -q -u "+quoteAll(names))
			return err
		},
	)
}
