package modules

import (
	"context"
	"fmt"

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
		return Changed(dest + " cloned"), nil
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
	if before == after {
		return Ok(dest + " already at " + after), nil
	}
	return Changed(fmt.Sprintf("%s updated %s -> %s", dest, before, after)), nil
}
