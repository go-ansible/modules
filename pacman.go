package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacman implements (a subset of) Ansible's `pacman` module:
// manages packages via Arch Linux's `pacman`.
//
// Args: name (string or []string) — cannot be combined with upgrade;
// state (present|installed|latest|absent|removed, default "present");
// update_cache (bool, default false) — runs `pacman -Sy --noconfirm`
// first; upgrade (bool, default false) — runs a full system upgrade
// (`pacman -Su --noconfirm`), mutually exclusive with name, always
// reported changed (a no-op system upgrade still exits 0).
//
// Simplifications vs real pacman: no `executable` (AUR helper),
// `extra_args`, `force`, `reason`/`reason_for`, `remove_nosave`,
// `root`/`cachedir`/`config` support. Idempotency for present/absent is
// checked via `pacman -Q <name>` (exit 0 iff installed); state=latest
// always runs `pacman -S --noconfirm <name>` and reports changed,
// matching this batch's house "can't cheaply tell already-latest
// apart" convention (see apt.go).
func modulePacman(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	upgrade := argBool(args, "upgrade", false)
	names := argStringList(args, "name")
	if upgrade && len(names) > 0 {
		return Result{}, errArg("pacman: name and upgrade are mutually exclusive")
	}

	if argBool(args, "update_cache", false) {
		if _, err := run(ctx, conn, "pacman -Sy --noconfirm"); err != nil {
			return Result{}, err
		}
	}

	if upgrade {
		if _, err := run(ctx, conn, "pacman -Su --noconfirm"); err != nil {
			return Result{}, err
		}
		return Changed("system upgraded"), nil
	}

	if len(names) == 0 {
		if s, err := requireString(args, "name"); err == nil {
			names = []string{s}
		} else {
			return Ok("nothing to do"), nil // update_cache-only run, matching real pacman
		}
	}
	state := argString(args, "state", "present")

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			res, err := conn.Exec(ctx, "pacman -Q "+shellQuote(name)+" >/dev/null 2>&1", nil)
			if err != nil {
				return false, fmt.Errorf("checking pacman package %s: %w", name, err)
			}
			return res.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pacman -S --noconfirm "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pacman -R --noconfirm "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pacman -S --noconfirm "+quoteAll(names))
			return err
		},
	)
}
