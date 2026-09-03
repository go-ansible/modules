package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleZypper implements (a subset of) Ansible's `zypper` module:
// manages packages on SUSE/openSUSE via `zypper` (with idempotency
// checks via `rpm`).
//
// Args: name (string or []string, alias pkg) — a package name, or list
// of names; a single name of "*" is only meaningful with
// state=dist-upgrade; state (present|installed|latest|absent|removed|
// dist-upgrade, default "present"); type (package|patch|pattern|
// product|srcpackage|application, default "package") — passed straight
// through as zypper's `--type`; disable_gpg_check (bool, default
// false); disable_recommends (bool, default true, matching real
// zypper's own default of modifying zypper's normal
// install-recommends behavior); update_cache (bool, alias refresh,
// default false) — runs `zypper refresh` first; transactional_update
// (bool, default false) — real zypper.py auto-detects "transactional
// update" systems (an immutable, btrfs-snapshotted root, common on
// SUSE MicroOS/Aeon) by checking for /usr/sbin/transactional-update
// plus a read-only btrfs root filesystem, and when detected wraps every
// zypper invocation in `transactional-update --continue
// --drop-if-no-change --quiet run` instead. This port has no portable
// way to probe the target's root filesystem type/read-only-ness through
// the Connection interface, so it exposes this as an explicit argument
// instead of auto-detecting it — a deliberate narrowing, not an
// oversight.
//
// Simplifications vs real zypper: no `allow_vendor_change`,
// `auto_import_keys`, `clean_deps`, `extra_args`,
// `extra_args_precommand`, `force`, `force_resolution`, `oldpackage`,
// `quiet`, `replacefiles`, `simple_errors`, or `skip_post_errors`
// support. Idempotency for present/absent is checked via `rpm -q
// <name>` (exit 0 iff installed) — this is only meaningful for
// type=package; for any other type this port cannot honestly tell
// whether the requested item is already present, so it always attempts
// the operation and reports changed for those, the same "can't cheaply
// tell already-satisfied apart" convention this batch's whole-system
// operations use elsewhere. state=latest always runs `zypper update`
// and reports changed for the same reason, and state=dist-upgrade
// (real zypper's whole-system upgrade-everything mode) always runs
// `zypper dist-upgrade` and reports changed.
func moduleZypper(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	state := argString(args, "state", "present")
	pkgType := argString(args, "type", "package")
	disableGPGCheck := argBool(args, "disable_gpg_check", false)
	disableRecommends := argBool(args, "disable_recommends", true)
	updateCache := argBool(args, "update_cache", argBool(args, "refresh", false))
	transactionalUpdate := argBool(args, "transactional_update", false)

	zBin := "zypper --non-interactive --quiet"
	if disableGPGCheck {
		zBin += " --no-gpg-checks"
	}
	if transactionalUpdate {
		zBin = "transactional-update --continue --drop-if-no-change --quiet run " + zBin
	}

	if updateCache {
		if _, err := run(ctx, conn, zBin+" refresh"); err != nil {
			return Result{}, err
		}
	}

	switch state {
	case "dist-upgrade":
		cmd := zBin + " dist-upgrade --auto-agree-with-licenses"
		if disableRecommends {
			cmd += " --no-recommends"
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("system upgraded"), nil

	case "present", "installed", "latest", "absent", "removed":
		if len(names) == 0 {
			return Ok("nothing to do"), nil
		}
		recommendsFlag := ""
		if disableRecommends {
			recommendsFlag = " --no-recommends"
		}
		return pkgManagerLoop(ctx, conn, names, state,
			func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
				if pkgType != "package" {
					// rpm -q cannot honestly answer for patches/
					// patterns/products/srcpackages/applications; see
					// the doc comment on moduleZypper.
					return false, nil
				}
				res, err := conn.Exec(ctx, "rpm -q "+shellQuote(name)+" >/dev/null 2>&1", nil)
				if err != nil {
					return false, fmt.Errorf("checking zypper package %s: %w", name, err)
				}
				return res.RC == 0, nil
			},
			func(ctx context.Context, conn remoteexec.Connection, names []string) error {
				_, err := run(ctx, conn, zBin+" install --type "+pkgType+" --auto-agree-with-licenses"+recommendsFlag+" -- "+quoteAll(names))
				return err
			},
			func(ctx context.Context, conn remoteexec.Connection, names []string) error {
				_, err := run(ctx, conn, zBin+" remove --type "+pkgType+" -- "+quoteAll(names))
				return err
			},
			func(ctx context.Context, conn remoteexec.Connection, names []string) error {
				_, err := run(ctx, conn, zBin+" update --type "+pkgType+" --auto-agree-with-licenses"+recommendsFlag+" -- "+quoteAll(names))
				return err
			},
		)

	default:
		return Result{}, errArg("zypper: state must be present, latest, absent, or dist-upgrade, got %q", state)
	}
}
