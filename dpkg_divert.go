package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDpkgDivert implements (a subset of) Ansible's `dpkg_divert`
// module (community.general): creates, removes, or updates a dpkg
// diversion — the mechanism by which only one package (or "LOCAL", the
// administrator) is allowed to install a file at a given path, forcing
// any other package's own copy aside — via `dpkg-divert --add`/
// `--remove`/`--listpackage`/`--truename`.
//
// Args: path (string, required) — the original, absolute file path;
// state (present|absent, default "present"); holder (string, optional)
// — the package name owning the diversion; empty or "LOCAL" means the
// diversion is held by "LOCAL" (dpkg's own reserved name for
// administrator-owned diversions); ignored when state=absent; divert
// (string, optional) — where the file is diverted to; defaults to
// `<path>.distrib`; ignored when state=absent; rename (bool, default
// false) — actually move the file aside/back when the diversion's
// state actually changes (a no-op when only holder/divert are being
// updated on an ALREADY-present diversion — see below); force (bool,
// default false) — when a rename would overwrite an existing file at
// the destination, remove that blocking file first and retry, rather
// than failing.
//
// Current state is read via `dpkg-divert --listpackage <path>` (empty
// output means no diversion; the holder name otherwise) and, if
// present, `dpkg-divert --truename <path>` for its divert location —
// matching real dpkg_divert.py's own `diversion_state()` exactly. If
// the computed desired state already matches, this is a no-op.
// Changing an EXISTING diversion's holder or divert location (without
// adding/removing it) is done by removing it and re-adding it with the
// new settings — matching real dpkg_divert.py's own approach — and,
// also matching real dpkg_divert.py exactly, that intermediate
// remove/re-add ALWAYS uses `--no-rename` for both steps regardless of
// the task's own `rename` argument (renaming only ever applies to an
// actual present<->absent transition, not an in-place settings
// update).
//
// This port does not replicate real dpkg_divert.py's own dpkg-divert
// version detection (`--listpackage` requires >=1.15.0; `--no-rename`
// requires >=1.19.1) — it always passes `--no-rename` when not
// renaming, assuming a modern dpkg-divert; a target with dpkg <1.19.1
// would receive an unrecognized flag, a real, documented gap versus
// upstream's own version-gated fallback. It also does not replicate
// real dpkg_divert.py's own two-phase "--test dry run, then decide
// whether an OSError-based manual file removal is needed" renaming
// logic; instead, when `force` is set and a rename-driven add/remove
// fails, this port removes the blocking file via a plain `rm -f` and
// retries the same dpkg-divert command once, which is simpler but
// achieves the same documented outcome ("existing contents of the file
// at this location are lost").
func moduleDpkgDivert(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	holder := argString(args, "holder", "")
	divertArg := argString(args, "divert", "")
	rename := argBool(args, "rename", false)
	force := argBool(args, "force", false)

	curExists, curHolder, curDivert, err := dpkgDivertState(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		wantHolder := "LOCAL"
		if holder != "" && holder != "LOCAL" {
			wantHolder = holder
		}
		wantDivert := divertArg
		if wantDivert == "" {
			wantDivert = path + ".distrib"
		}

		if curExists && curHolder == wantHolder && curDivert == wantDivert {
			return Ok(fmt.Sprintf("diversion of %s already set", path)), nil
		}

		var commands []string
		if curExists {
			// Updating an existing diversion's holder/divert: remove
			// then re-add, always --no-rename for both steps.
			rmCmd := "dpkg-divert --no-rename --remove " + shellQuote(path)
			commands = append(commands, rmCmd)
			if err := dpkgDivertRun(ctx, conn, rmCmd, "", false); err != nil {
				return Result{}, err
			}
			addCmd := dpkgDivertAddCmd(path, wantHolder, wantDivert, false)
			commands = append(commands, addCmd)
			if err := dpkgDivertRun(ctx, conn, addCmd, wantDivert, force); err != nil {
				return Result{}, err
			}
		} else {
			addCmd := dpkgDivertAddCmd(path, wantHolder, wantDivert, rename)
			commands = append(commands, addCmd)
			if err := dpkgDivertRun(ctx, conn, addCmd, wantDivert, force); err != nil {
				return Result{}, err
			}
		}
		return Changed(fmt.Sprintf("diversion of %s to %s set (holder %s)", path, wantDivert, wantHolder)).
			WithExtra("commands", commands), nil

	case "absent":
		if !curExists {
			return Ok(fmt.Sprintf("no diversion of %s", path)), nil
		}
		renameFlag := "--no-rename"
		if rename {
			renameFlag = "--rename"
		}
		cmd := "dpkg-divert " + renameFlag + " --remove " + shellQuote(path)
		if err := dpkgDivertRun(ctx, conn, cmd, path, force); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("diversion of %s removed", path)).WithExtra("commands", []string{cmd}), nil

	default:
		return Result{}, errArg("dpkg_divert: state must be present or absent, got %q", state)
	}
}

func dpkgDivertState(ctx context.Context, conn remoteexec.Connection, path string) (exists bool, holder, divert string, err error) {
	out, err := run(ctx, conn, "dpkg-divert --listpackage "+shellQuote(path))
	if err != nil {
		return false, "", "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return false, "", "", nil
	}
	divertOut, err := run(ctx, conn, "dpkg-divert --truename "+shellQuote(path))
	if err != nil {
		return false, "", "", err
	}
	return true, out, strings.TrimSpace(divertOut), nil
}

func dpkgDivertAddCmd(path, holder, divert string, rename bool) string {
	cmd := "dpkg-divert"
	if rename {
		cmd += " --rename"
	} else {
		cmd += " --no-rename"
	}
	if holder == "LOCAL" {
		cmd += " --local"
	} else {
		cmd += " --package " + shellQuote(holder)
	}
	cmd += " --divert " + shellQuote(divert) + " --add " + shellQuote(path)
	return cmd
}

// dpkgDivertRun runs cmd, retrying once (after `rm -f blockingFile`) if
// it fails and force is set. blockingFile may be empty to disable the
// retry (used for the --no-rename removal step of an in-place update,
// which never needs it).
func dpkgDivertRun(ctx context.Context, conn remoteexec.Connection, cmd, blockingFile string, force bool) error {
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return err
	}
	if res.RC == 0 {
		return nil
	}
	if force && blockingFile != "" {
		if _, rmErr := run(ctx, conn, "rm -f "+shellQuote(blockingFile)); rmErr != nil {
			return rmErr
		}
		res, err = runStatus(ctx, conn, cmd)
		if err != nil {
			return err
		}
		if res.RC == 0 {
			return nil
		}
	}
	return fmt.Errorf("dpkg_divert: %s: exit %d: %s", cmd, res.RC, strings.TrimSpace(res.Stderr))
}
