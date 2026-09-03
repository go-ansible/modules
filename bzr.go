package modules

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleBzr implements (a subset of) Ansible's `bzr` module: clones a
// Bazaar branch, or (if already cloned) resets any local modifications
// and pulls+switches it to the requested version — read from real
// bzr.py's own Bzr class (this batch's hard rule: the exact
// clone/reset/pull/switch_version command sequence and ordering are
// only visible in the implementation, not EXAMPLES/OPTIONS).
//
// Args: name (string, required) — the parent branch's SSH or HTTP
// address; real bzr aliases this from `parent`, but this port only
// accepts `name`, matching acl.go's own documented "args are already
// resolved by the caller before reaching a module" convention; dest
// (string, required); version (string, default "head") — a bzr revno
// or revid; force (bool, default false) — discard local modifications
// rather than failing when present; executable (string, default
// "bzr") — path to the `bzr` binary to use, resolved via PATH on the
// target when just "bzr" (matching real bzr's own get_bin_path
// fallback).
//
// Existence is checked via dest+"/.bzr/branch/branch.conf" (real
// bzr's own check, not just dest+"/.bzr"). A fresh clone `mkdir -p`s
// dest's parent directory (matching real clone()'s own os.makedirs),
// then runs `bzr branch [-r version] name dest`. An already-cloned
// branch: checks for local modifications via `bzr status -S` (any
// line NOT starting with "??" means a tracked-file modification —
// matching real has_local_mods' own mods_re filter, which keeps only
// non-"??" lines), fails with "Local modifications exist in branch
// (force=false)." (a Result{Failed:true}, since this is a well-formed
// request the module determined it cannot satisfy — not a Go error)
// when any are found and force is false; otherwise runs `bzr revert`
// to discard them, then `bzr pull [-r version]`. Either way, `bzr
// revert [-r version]` is run last to switch to the requested version
// (matching real switch_version — yes, revert is used for both
// discarding local mods AND switching version, exactly as real bzr.py
// does).
//
// changed is true whenever `bzr revno` differs before/after, OR local
// modifications were found and discarded on an already-cloned branch
// (matching real main()'s own `before != after or local_mods` — a
// rare case where "changed" does not imply the working tree's own
// revno moved). Returns Extra["before"]/Extra["after"] (both "" on a
// fresh clone, matching real main()'s own `before = None` in that
// case).
func moduleBzr(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	version := argString(args, "version", "head")
	force := argBool(args, "force", false)
	bzrPath := argString(args, "executable", "bzr")
	if bzrPath == "" {
		bzrPath = "bzr"
	}

	bzrconfig := dest + "/.bzr/branch/branch.conf"
	exists, err := pathExists(ctx, conn, bzrconfig)
	if err != nil {
		return Result{}, err
	}

	localMods := false
	var before string
	if !exists {
		parent := filepath.Dir(dest)
		if _, err := run(ctx, conn, "mkdir -p "+shellQuote(parent)); err != nil {
			return Result{}, err
		}
		cmd := bzrPath + " branch " + bzrVersionFlag(version) + shellQuote(name) + " " + shellQuote(dest)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
	} else {
		localMods, err = bzrHasLocalMods(ctx, conn, bzrPath, dest)
		if err != nil {
			return Result{}, err
		}
		before, err = bzrRevno(ctx, conn, bzrPath, dest)
		if err != nil {
			return Result{}, err
		}
		if !force && localMods {
			return Fail("Local modifications exist in branch (force=false)."), nil
		}
		if _, err := run(ctx, conn, bzrIn(dest, bzrPath+" revert")); err != nil {
			return Result{}, err
		}
		pullCmd := bzrPath + " pull " + strings.TrimSpace(bzrVersionFlag(version))
		if _, err := run(ctx, conn, bzrIn(dest, strings.TrimRight(pullCmd, " "))); err != nil {
			return Result{}, err
		}
	}

	switchCmd := bzrPath + " revert " + strings.TrimSpace(bzrVersionFlag(version))
	if _, err := run(ctx, conn, bzrIn(dest, strings.TrimRight(switchCmd, " "))); err != nil {
		return Result{}, err
	}

	after, err := bzrRevno(ctx, conn, bzrPath, dest)
	if err != nil {
		return Result{}, err
	}

	changed := before != after || localMods
	r := Ok("")
	if changed {
		r = Changed("")
	}
	return r.WithExtra("before", before).WithExtra("after", after), nil
}

// bzrVersionFlag renders the "-r <version> " flag, or "" for "head"
// (case-insensitively, matching real bzr's own `version.lower() !=
// "head"` checks throughout).
func bzrVersionFlag(version string) string {
	if strings.EqualFold(version, "head") {
		return ""
	}
	return "-r " + shellQuote(version) + " "
}

func bzrIn(dir, cmd string) string {
	return "cd " + shellQuote(dir) + " && " + cmd
}

func bzrRevno(ctx context.Context, conn remoteexec.Connection, bzrPath, dest string) (string, error) {
	return run(ctx, conn, bzrIn(dest, bzrPath+" revno"))
}

var bzrUntrackedPattern = regexp.MustCompile(`^\?\?`)

// bzrHasLocalMods reports whether `bzr status -S` shows any tracked-file
// modification (a line not starting with "??", which marks an untracked
// file) — matching real has_local_mods exactly.
func bzrHasLocalMods(ctx context.Context, conn remoteexec.Connection, bzrPath, dest string) (bool, error) {
	res, err := runStatus(ctx, conn, bzrIn(dest, bzrPath+" status -S"))
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if line == "" {
			continue
		}
		if !bzrUntrackedPattern.MatchString(line) {
			return true, nil
		}
	}
	return false, nil
}
