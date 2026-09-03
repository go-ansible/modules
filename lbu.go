package modules

import (
	"context"
	"path"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLbu implements Ansible's `lbu` (community.general) module:
// manages Alpine Linux's Local Backup Utility (`lbu`), which tracks
// which paths get committed into the boot-time overlay used by
// run-from-RAM Alpine installs.
//
// Args: commit (bool, tri-state — a nil/unset/false value all mean "do
// not commit", matching real lbu's own plain truthiness check on
// module.params["commit"]); include ([]string, optional) — paths to add
// to lbu's include list; exclude ([]string, optional) — paths to add to
// lbu's exclude list.
//
// Faithfully mirrors real lbu's own two-phase logic:
//  1. For each of include/exclude given, this port runs `lbu <list> -l`
//     to fetch that list's own currently-tracked paths, and normalizes
//     each requested path the same way real lbu's own
//     os.path.normpath(f"/{path}")[1:] does, to detect whether an
//     update is actually needed.
//  2. If any requested path is not yet tracked, `lbu include <p...>` /
//     `lbu exclude <p...>` is run for each list that was given.
//  3. If commit is truthy, this port commits when either step 2 already
//     ran OR `lbu status` reports any pending change (non-empty
//     stdout) — matching real lbu's own `run_lbu("status") > ""` check.
//
// check_mode is not modeled (see zfs_delegate_admin.go's own doc
// comment for this port's general convention there).
func moduleLbu(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	include := argStringList(args, "include")
	exclude := argStringList(args, "exclude")
	wantCommit := argBool(args, "commit", false)

	update := false
	for _, param := range []struct {
		key   string
		paths []string
	}{{"include", include}, {"exclude", exclude}} {
		if len(param.paths) == 0 {
			continue
		}
		out, err := run(ctx, conn, "lbu "+param.key+" -l")
		if err != nil {
			return Result{}, err
		}
		tracked := splitLines(out)
		for _, p := range param.paths {
			if !sliceHasString(tracked, lbuNormalize(p)) {
				update = true
			}
		}
	}

	commit := false
	if wantCommit {
		if update {
			commit = true
		} else {
			out, err := run(ctx, conn, "lbu status")
			if err != nil {
				return Result{}, err
			}
			commit = strings.TrimSpace(out) != ""
		}
	}

	changed := false
	if update {
		for _, param := range []struct {
			key   string
			paths []string
		}{{"include", include}, {"exclude", exclude}} {
			if len(param.paths) == 0 {
				continue
			}
			if _, err := run(ctx, conn, "lbu "+param.key+" "+quoteAll(param.paths)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}
	if commit {
		if _, err := run(ctx, conn, "lbu commit"); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed("lbu updated"), nil
	}
	return Ok("lbu unchanged"), nil
}

// lbuNormalize matches real lbu's own os.path.normpath(f"/{path}")[1:]:
// force the path absolute (so relative and absolute inputs compare the
// same way against lbu's own always-absolute listing), clean it, then
// drop the leading "/".
func lbuNormalize(p string) string {
	return strings.TrimPrefix(path.Clean("/"+p), "/")
}
