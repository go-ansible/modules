package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAptRepo implements Ansible's `apt_repo` module: manages a
// repository definition for ALT Linux's own `apt-repo` tool — see
// https://www.altlinux.org/Apt-repo. This is a different, unrelated
// module from apt_repository.go's own `apt_repository` (Debian/
// Ubuntu's PPA/deb-line manager): `apt-repo` is ALT Linux's own
// third-party wrapper around its `apt` configuration, with a
// completely different CLI (`apt-repo add|rm|update`, no PPA concept,
// no derived sources.list.d filename), matching real apt_repo's own
// NOTE: "This module works on ALT based distros."
//
// Args: repo (string, required) — a repository name/spec in whatever
// form `apt-repo add` accepts (including the special name "all" for
// state=absent, and local repo specs like "copy:///path", per real
// apt_repo's own EXAMPLES — this port passes repo through verbatim,
// exactly like real apt_repo, without interpreting its syntax at all).
// state (present|absent, default "present"). remove_others (bool,
// default false) — only meaningful with state=present: adds repo, THEN
// removes every other repository, THEN re-adds repo, matching real
// apt_repo's own exact three-call `set_repo()` sequence (add validates
// repo BEFORE wiping everything else, so a bad repo spec fails without
// having removed anything). update (bool, default false) — runs
// `apt-repo update` afterward.
//
// Real apt_repo has NO check_mode support at all (real module's own
// attributes.check_mode.support: none, with its own NOTE explicitly
// blaming "a limitation in the apt-repo tool") — this port has no
// check_mode support anywhere (a runtime-engine concern outside every
// module's own Func signature here), so that lines up without any
// extra work.
//
// Changed is computed exactly like real apt_repo: by comparing bare
// `apt-repo`'s own output (its own repository listing) before and
// after this task's calls, string-for-string — not by inspecting rc or
// guessing from state, matching real apt_repo's own `changed =
// old_repositories != new_repositories`. Every apt-repo invocation is
// run with LANGUAGE=C LC_ALL=C, matching real apt_repo's own
// `module.run_command_environ_update`.
func moduleAptRepo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	repo, err := requireString(args, "repo")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("apt_repo: state must be present or absent, got %q", state)
	}
	removeOthers := argBool(args, "remove_others", false)
	update := argBool(args, "update", false)

	exists, err := pathExists(ctx, conn, "/usr/bin/apt-repo")
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("apt_repo: cannot find /usr/bin/apt-repo"), nil
	}

	before, err := aptRepoRun(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	if state == "present" {
		if removeOthers {
			if _, err := aptRepoRun(ctx, conn, "add", repo); err != nil {
				return Result{}, err
			}
			if _, err := aptRepoRun(ctx, conn, "rm", "all"); err != nil {
				return Result{}, err
			}
			if _, err := aptRepoRun(ctx, conn, "add", repo); err != nil {
				return Result{}, err
			}
		} else {
			if _, err := aptRepoRun(ctx, conn, "add", repo); err != nil {
				return Result{}, err
			}
		}
	} else {
		if _, err := aptRepoRun(ctx, conn, "rm", repo); err != nil {
			return Result{}, err
		}
	}

	if update {
		if _, err := aptRepoRun(ctx, conn, "update"); err != nil {
			return Result{}, err
		}
	}

	after, err := aptRepoRun(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	changed := before != after
	result := Ok("unchanged")
	if changed {
		result = Changed(repo)
	}
	result = result.WithExtra("repo", repo).WithExtra("state", state)
	return result, nil
}

// aptRepoRun runs `apt-repo <args...>` (or bare `apt-repo` for args ==
// nil, real apt_repo's own way of snapshotting the current repository
// listing) with LANGUAGE=C LC_ALL=C, treating a non-zero exit as a Go
// error since this is always this module's own internal command, never
// a user-supplied one whose exit code is the point (matching run()'s
// own documented convention).
func aptRepoRun(ctx context.Context, conn remoteexec.Connection, args ...string) (string, error) {
	cmd := "env LANGUAGE=C LC_ALL=C /usr/bin/apt-repo"
	for _, a := range args {
		cmd += " " + shellQuote(a)
	}
	return run(ctx, conn, cmd)
}
