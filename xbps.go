package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXbps implements (a subset of) Ansible's `xbps` module: manages
// packages on Void Linux via `xbps-install`/`xbps-remove`/`xbps-query`.
//
// Args: name (string or []string, aliases pkg/package) — a package
// name, or list of names; state (present|installed|absent|removed|
// latest, default "present"); update_cache (bool, default true) — runs
// `xbps-install -Sy` (sync repository indexes) before an install/latest
// operation, matching real xbps's own update_cache-defaults-true
// (a deliberate deviation from this batch's usual update_cache-defaults-
// false convention, kept because it matches real xbps.py exactly rather
// than this port's house default); upgrade (bool, default false) — runs
// a whole-system `xbps-install -Suy`, mutually exclusive with name,
// always reported changed (a no-op system upgrade still exits 0).
//
// Simplifications vs real xbps: no `accept_pubkey` (auto-accepting
// unknown repo signing keys), `recurse` (removing now-unneeded
// dependencies), `repositories` (extra repo URLs for this invocation),
// `root` (alternate root directory), or `upgrade_xbps` (real xbps
// probes and self-upgrades the xbps package itself before other
// operations when out of date; this port does not replicate that
// probe) support. Idempotency for present/absent is checked via
// `xbps-query <name>` (exit 0 iff installed); state=latest always runs
// `xbps-install -uy <name>` and reports changed, matching this batch's
// house "can't cheaply tell already-latest apart" convention.
func moduleXbps(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	upgrade := argBool(args, "upgrade", false)
	names := argStringList(args, "name")
	if upgrade && len(names) > 0 {
		return Result{}, errArg("xbps: name and upgrade are mutually exclusive")
	}
	updateCache := argBool(args, "update_cache", true)

	if updateCache {
		if _, err := run(ctx, conn, "xbps-install -Sy"); err != nil {
			return Result{}, err
		}
	}

	if upgrade {
		if _, err := run(ctx, conn, "xbps-install -Suy"); err != nil {
			return Result{}, err
		}
		return Changed("system upgraded"), nil
	}

	if len(names) == 0 {
		if s, err := requireString(args, "name"); err == nil {
			names = []string{s}
		} else {
			return Ok("nothing to do"), nil
		}
	}
	state := argString(args, "state", "present")

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			res, err := conn.Exec(ctx, "xbps-query "+shellQuote(name)+" >/dev/null 2>&1", nil)
			if err != nil {
				return false, fmt.Errorf("checking xbps package %s: %w", name, err)
			}
			return res.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "xbps-install -y "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "xbps-remove -y "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "xbps-install -uy "+quoteAll(names))
			return err
		},
	)
}
