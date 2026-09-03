package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// pkgManagerLoop implements the install/absent/latest control flow shared
// by this batch's simple list-of-names package managers (apk, homebrew,
// homebrew_cask, snap, flatpak, pacman) — the same query-then-act
// idempotency house pattern apt.go/dnf.go/pip.go already established:
// query each name's presence, then batch-act on only the names that need
// it. latest may be nil for a package manager whose real module has no
// "latest" state; state=="latest" then fails with errArg rather than
// silently falling through to "present"'s behavior.
func pkgManagerLoop(ctx context.Context, conn remoteexec.Connection, names []string, state string,
	query func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error),
	install func(ctx context.Context, conn remoteexec.Connection, names []string) error,
	remove func(ctx context.Context, conn remoteexec.Connection, names []string) error,
	latest func(ctx context.Context, conn remoteexec.Connection, names []string) error,
) (Result, error) {
	switch state {
	case "absent", "removed", "uninstalled":
		var toRemove []string
		for _, n := range names {
			present, err := query(ctx, conn, n)
			if err != nil {
				return Result{}, err
			}
			if present {
				toRemove = append(toRemove, n)
			}
		}
		if len(toRemove) == 0 {
			return Ok("already absent"), nil
		}
		if err := remove(ctx, conn, toRemove); err != nil {
			return Result{}, err
		}
		return Changed(strings.Join(toRemove, ", ")), nil

	case "latest", "upgraded":
		if latest == nil {
			return Result{}, errArg("state=%s is not supported by this module", state)
		}
		if err := latest(ctx, conn, names); err != nil {
			return Result{}, err
		}
		// A no-op upgrade still exits 0; like apt's own "latest" branch,
		// we can't cheaply tell "already latest" apart without parsing
		// tool-specific output, so this is always reported changed.
		return Changed(strings.Join(names, ", ")), nil

	default: // "present" / "installed", and any tool-specific alias of it
		var toInstall []string
		for _, n := range names {
			present, err := query(ctx, conn, n)
			if err != nil {
				return Result{}, err
			}
			if !present {
				toInstall = append(toInstall, n)
			}
		}
		if len(toInstall) == 0 {
			return Ok("already installed"), nil
		}
		if err := install(ctx, conn, toInstall); err != nil {
			return Result{}, err
		}
		return Changed(strings.Join(toInstall, ", ")), nil
	}
}

// resolveNames extracts the "name" argument as a string list, accepting
// either a single string or a list, matching apt.go/pip.go's own
// convention for this shape.
func resolveNames(args map[string]any) ([]string, error) {
	names := argStringList(args, "name")
	if len(names) > 0 {
		return names, nil
	}
	if s, err := requireString(args, "name"); err == nil {
		return []string{s}, nil
	}
	return nil, errArg("missing required argument: name")
}
