package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGit implements (a subset of) Ansible's `git` module: clones a
// repository, or updates an existing clone to the requested version.
//
// Args: repo (string, required); dest (string, required); version
// (string, default "HEAD" — a branch, tag, or commit).
func moduleGit(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	repo, err := requireString(args, "repo")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	version := argString(args, "version", "HEAD")

	exists, err := pathExists(ctx, conn, dest+"/.git")
	if err != nil {
		return Result{}, err
	}

	if !exists {
		cmd := "git clone --quiet " + shellQuote(repo) + " " + shellQuote(dest)
		if version != "HEAD" {
			cmd = "git clone --quiet --branch " + shellQuote(version) + " " + shellQuote(repo) + " " + shellQuote(dest)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		// Real's git reports `before` and `after` -- the commit the
		// checkout was at and the one it is at now -- and NO msg.
		// Measured on a fresh clone: before is the EMPTY string (there
		// was no prior checkout) and after is the cloned HEAD. This
		// returned a sentence with the dest path in msg instead.
		head, herr := run(ctx, conn, "git -C "+shellQuote(dest)+" rev-parse HEAD")
		if herr != nil {
			return Result{}, herr
		}
		return gitResult(true, "", head), nil
	}

	before, err := run(ctx, conn, "git -C "+shellQuote(dest)+" rev-parse HEAD")
	if err != nil {
		return Result{}, err
	}

	if _, err := run(ctx, conn, "git -C "+shellQuote(dest)+" fetch --quiet --tags origin"); err != nil {
		return Result{}, err
	}
	checkoutTarget := version
	if version == "HEAD" {
		checkoutTarget = "origin/HEAD"
	}
	if _, err := run(ctx, conn, "git -C "+shellQuote(dest)+" checkout --quiet "+shellQuote(checkoutTarget)); err != nil {
		return Result{}, err
	}

	after, err := run(ctx, conn, "git -C "+shellQuote(dest)+" rev-parse HEAD")
	if err != nil {
		return Result{}, err
	}
	return gitResult(before != after, before, after), nil
}

// gitResult is real's shape: before, after, changed, failed -- and no
// msg, so `r.msg` after a real git task is an undefined variable.
// Measured key set on a fresh clone: after,before,changed,failed.
func gitResult(changed bool, before, after string) Result {
	r := Ok("")
	if changed {
		r = Changed("")
	}
	r.NoMsg = true
	return r.WithExtra("before", before).WithExtra("after", after)
}
