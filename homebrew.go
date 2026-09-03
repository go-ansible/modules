package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHomebrew implements (a subset of) Ansible's `homebrew` module:
// manages Homebrew formulae on macOS/Linuxbrew.
//
// Args: name (string or []string, required); state (present|installed|
// absent|removed|uninstalled|latest|upgraded|head|linked|unlinked,
// default "present"); update_homebrew (bool, default false) — runs
// `brew update` first.
//
// Simplifications vs real homebrew: no `path`, `install_options`,
// `upgrade_options`, `upgrade_all`, or `force_formula` support.
// Idempotency for present/absent is checked via `brew list --formula
// <name>` (exit 0 iff installed); state=latest/upgraded always runs
// `brew upgrade <name>` and reports changed (a no-op upgrade still
// exits 0, the same "can't cheaply tell apart" limitation apt.go/dnf.go
// document for their own latest branch). state=head always (re)installs
// with `--HEAD` and reports changed, since a HEAD install's own
// freshness can't be probed cheaply either. state=linked/unlinked map
// directly to `brew link`/`brew unlink`, always reported changed for
// the same reason (their own idempotency is homebrew's job, not
// probed here).
func moduleHomebrew(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return homebrewLike(ctx, conn, args, "")
}

// homebrewLike implements moduleHomebrew and (with formulaFlag ==
// "--cask") moduleHomebrewCask, since the two modules are the same
// install/remove/query shape aimed at formulae vs casks.
func homebrewLike(ctx context.Context, conn remoteexec.Connection, args map[string]any, formulaFlag string) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("homebrew: %v", err)
	}
	state := argString(args, "state", "present")
	flag := formulaFlag
	if flag != "" {
		flag += " "
	}

	if argBool(args, "update_homebrew", false) {
		if _, err := run(ctx, conn, "brew update"); err != nil {
			return Result{}, err
		}
	}

	switch state {
	case "head":
		if _, err := run(ctx, conn, "brew install "+flag+"--HEAD "+quoteAll(names)); err != nil {
			return Result{}, err
		}
		return Changed("installed HEAD"), nil

	case "linked":
		if _, err := run(ctx, conn, "brew link "+quoteAll(names)); err != nil {
			return Result{}, err
		}
		return Changed("linked"), nil

	case "unlinked":
		if _, err := run(ctx, conn, "brew unlink "+quoteAll(names)); err != nil {
			return Result{}, err
		}
		return Changed("unlinked"), nil
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			listFlag := "--formula"
			if formulaFlag == "--cask" {
				listFlag = "--cask"
			}
			res, err := conn.Exec(ctx, "brew list "+listFlag+" "+shellQuote(name)+" >/dev/null 2>&1", nil)
			if err != nil {
				return false, fmt.Errorf("checking homebrew package %s: %w", name, err)
			}
			return res.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "brew install "+flag+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "brew uninstall "+flag+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "brew upgrade "+flag+quoteAll(names))
			return err
		},
	)
}
