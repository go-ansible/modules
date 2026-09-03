package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHg implements (a subset of) Ansible's `hg` module: clones,
// pulls, updates, or just inspects a Mercurial repository — read from
// real hg.py's own Hg class (this batch's hard rule: the exact
// clone/pull/update/cleanup command sequence and short-circuit cases
// are only visible in the implementation, not EXAMPLES/OPTIONS).
//
// Args: repo (string, required) — real hg aliases this from `name`,
// not accepted here, matching acl.go's own documented convention; dest
// (string, required unless clone=false and update=false); revision
// (string, optional) — real hg aliases this from `version`, not
// accepted here; force (bool, default false) — discards uncommitted
// changes via `hg update -C -r .`; purge (bool, default false) —
// deletes untracked files via `hg purge`; update (bool, default true)
// — if false, never pulls/updates an existing clone; clone (bool,
// default true) — if false, never clones a missing one; executable
// (string, default "hg").
//
// Five cases, matching real main()'s own dispatch exactly:
//
//  1. clone=false and update=false: returns immediately with
//     Extra["after"] set to `hg id <repo>` (querying the REMOTE
//     repository, never touching dest at all — dest is not even
//     required in this case).
//  2. dest has no .hg/hgrc yet: clones if clone=true (`hg clone repo
//     dest [-r revision]`), otherwise reports unchanged.
//  3. update=false but the clone already exists: reports the existing
//     `hg id -b -i -t -R dest` as both before and after, unchanged.
//  4. the clone is already at the requested revision (only checked
//     when revision is non-empty and at least 7 characters — matching
//     real at_revision's own "assume anything shorter is a rev
//     number/tag/branch, not a full changeset hash" heuristic, via
//     `hg --debug id -i -R dest` prefix-matching revision): no pull,
//     but force/purge cleanup still runs.
//  5. otherwise: cleanup runs, then `hg pull -R dest repo`, then `hg
//     update [-r revision] -R dest`.
//
// changed is before != after (comparing `hg id -b -i -t -R dest`
// output, which embeds a trailing "+" when the working copy has
// uncommitted changes) OR cleaned (force discarded local mods, or
// purge removed untracked files) — matching real main()'s own
// `before != after or cleaned` exactly, the same "changed does not
// necessarily mean the revision moved" shape as bzr.go's own
// before/after-or-local-mods rule.
func moduleHg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	repo, err := requireString(args, "repo")
	if err != nil {
		return Result{}, err
	}
	dest := argString(args, "dest", "")
	revision := argString(args, "revision", "")
	force := argBool(args, "force", false)
	purge := argBool(args, "purge", false)
	update := argBool(args, "update", true)
	clone := argBool(args, "clone", true)
	hgPath := argString(args, "executable", "hg")
	if hgPath == "" {
		hgPath = "hg"
	}

	if dest == "" && (clone || update) {
		return Result{}, errArg("hg: dest is required unless clone=false and update=false")
	}

	if !clone && !update {
		after, err := run(ctx, conn, hgPath+" id "+shellQuote(repo))
		if err != nil {
			return Result{}, err
		}
		return Ok("").WithExtra("after", after), nil
	}

	hgrc := dest + "/.hg/hgrc"
	hgrcExists, err := pathExists(ctx, conn, hgrc)
	if err != nil {
		return Result{}, err
	}

	var before string
	cleaned := false

	switch {
	case !hgrcExists:
		if !clone {
			return Ok(""), nil
		}
		cmd := hgPath + " clone " + shellQuote(repo) + " " + shellQuote(dest)
		if revision != "" {
			cmd += " -r " + shellQuote(revision)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
	case !update:
		before, err = hgRevision(ctx, conn, hgPath, dest)
		if err != nil {
			return Result{}, err
		}
	default:
		atRev, err := hgAtRevision(ctx, conn, hgPath, dest, revision)
		if err != nil {
			return Result{}, err
		}
		before, err = hgRevision(ctx, conn, hgPath, dest)
		if err != nil {
			return Result{}, err
		}
		cleaned, err = hgCleanup(ctx, conn, hgPath, dest, force, purge)
		if err != nil {
			return Result{}, err
		}
		if !atRev {
			if _, err := run(ctx, conn, hgPath+" pull -R "+shellQuote(dest)+" "+shellQuote(repo)); err != nil {
				return Result{}, err
			}
			updateCmd := hgPath + " update -R " + shellQuote(dest)
			if revision != "" {
				updateCmd = hgPath + " update -r " + shellQuote(revision) + " -R " + shellQuote(dest)
			}
			if _, err := run(ctx, conn, updateCmd); err != nil {
				return Result{}, err
			}
		}
	}

	after, err := hgRevision(ctx, conn, hgPath, dest)
	if err != nil {
		return Result{}, err
	}

	changed := before != after || cleaned
	r := Ok("")
	if changed {
		r = Changed("")
	}
	return r.WithExtra("before", before).WithExtra("after", after).WithExtra("cleaned", cleaned), nil
}

func hgRevision(ctx context.Context, conn remoteexec.Connection, hgPath, dest string) (string, error) {
	return run(ctx, conn, hgPath+" id -b -i -t -R "+shellQuote(dest))
}

// hgAtRevision reports whether dest is already checked out at
// revision, matching real Hg.at_revision's own "only meaningful for a
// full changeset hash, at least 7 characters" heuristic (anything
// shorter is assumed to be a revision number, branch name, or tag,
// which `hg --debug id -i` never returns, so it would never match
// anyway — real code short-circuits to false rather than issuing the
// command in that case, reproduced here for the same reason).
func hgAtRevision(ctx context.Context, conn remoteexec.Connection, hgPath, dest, revision string) (bool, error) {
	if revision == "" || len(revision) < 7 {
		return false, nil
	}
	out, err := run(ctx, conn, hgPath+" --debug id -i -R "+shellQuote(dest))
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(out, revision), nil
}

// hgHasLocalMods reports whether dest's working copy has uncommitted
// changes, via the trailing "+" `hg id -b -i -t` embeds when it does
// (matching real has_local_mods' own `"+" in now` check).
func hgHasLocalMods(ctx context.Context, conn remoteexec.Connection, hgPath, dest string) (bool, error) {
	rev, err := hgRevision(ctx, conn, hgPath, dest)
	if err != nil {
		return false, err
	}
	return strings.Contains(rev, "+"), nil
}

// hgCleanup implements real Hg.cleanup: force discards uncommitted
// changes (`hg update -C -R dest -r .`, only run when there actually
// are local mods, matching real discard()'s own early-return), purge
// deletes untracked files (only when `hg purge ... --print` actually
// lists any, matching real purge()'s own "only act on real work" — a
// no-op purge should not be reported as a change). Returns whether
// either action actually changed anything.
func hgCleanup(ctx context.Context, conn remoteexec.Connection, hgPath, dest string, force, purge bool) (bool, error) {
	discarded := false
	purged := false

	if force {
		hadMods, err := hgHasLocalMods(ctx, conn, hgPath, dest)
		if err != nil {
			return false, err
		}
		if hadMods {
			if _, err := run(ctx, conn, hgPath+" update -C -R "+shellQuote(dest)+" -r ."); err != nil {
				return false, err
			}
			stillHasMods, err := hgHasLocalMods(ctx, conn, hgPath, dest)
			if err != nil {
				return false, err
			}
			discarded = !stillHasMods
		}
	}
	if purge {
		untracked, err := run(ctx, conn, hgPath+" purge --config extensions.purge= -R "+shellQuote(dest)+" --print")
		if err != nil {
			return false, err
		}
		if untracked != "" {
			if _, err := run(ctx, conn, hgPath+" purge --config extensions.purge= -R "+shellQuote(dest)); err != nil {
				return false, err
			}
			purged = true
		}
	}
	return discarded || purged, nil
}
