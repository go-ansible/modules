package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSubversion implements (a subset of) Ansible's `subversion`
// module: checks out (or exports, or updates an existing checkout of)
// a Subversion repository — the svn counterpart to git.go, following
// its same shape.
//
// Args: repo (string, required — real subversion also accepts this as
// `name`/`repository`; this port only accepts `repo`, since aliasing is
// resolved by the caller before args reach a module — see module.go's
// package doc comment); dest (string, required); revision (string,
// default "HEAD" — a real svn revision keyword/number); export (bool,
// default false) — export a clean tree with no .svn metadata instead
// of a working checkout.
//
// Simplifications vs real subversion: no `force` (discard local
// modifications), `in_place`, `switch`, `password`, or `executable`
// override support. export=true does NOT check whether dest already
// holds the wanted export — it always re-exports and reports changed,
// the same "can't cheaply tell already-there apart, so always act"
// tradeoff apt_repository's PPA path and dnf/apt's "latest" state make
// elsewhere in this package (a real export has no metadata directory
// like .svn to probe for staleness the way a checkout's svnversion
// does).
func moduleSubversion(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	repo, err := requireString(args, "repo")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	revision := argString(args, "revision", "HEAD")
	export := argBool(args, "export", false)

	revFlag := ""
	if revision != "HEAD" {
		revFlag = "-r " + shellQuote(revision) + " "
	}

	if export {
		cmd := "svn export --quiet --force " + revFlag + shellQuote(repo) + " " + shellQuote(dest)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(dest + " exported"), nil
	}

	exists, err := pathExists(ctx, conn, dest+"/.svn")
	if err != nil {
		return Result{}, err
	}

	if !exists {
		cmd := "svn checkout --quiet " + revFlag + shellQuote(repo) + " " + shellQuote(dest)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(dest + " checked out"), nil
	}

	before, err := run(ctx, conn, "svnversion "+shellQuote(dest))
	if err != nil {
		return Result{}, err
	}
	cmd := "svn update --quiet " + revFlag + shellQuote(dest)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	after, err := run(ctx, conn, "svnversion "+shellQuote(dest))
	if err != nil {
		return Result{}, err
	}
	if before == after {
		return Ok(dest + " already at " + after), nil
	}
	return Changed(fmt.Sprintf("%s updated %s -> %s", dest, before, after)), nil
}
