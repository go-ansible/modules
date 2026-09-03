package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCopr implements Ansible's `copr` (community.general) module:
// enables, disables, or removes a Fedora COPR repository.
//
// Architectural note: real copr's own implementation does not shell out
// to a CLI at all — it imports dnf's Python API in-process, downloads
// the repository's .repo content directly from the copr host over HTTP
// (`_download_repo_info`), and writes/edits /etc/yum.repos.d/*.repo
// itself, comparing file content byte-for-byte for idempotency and
// editing dnf's own repo config sections for the disabled state. None
// of that is reachable through this port's Connection (Exec/Put/Fetch
// only, no in-process Python/dnf), so this port instead composes the
// `dnf copr` CLI subcommand (from the dnf-plugins-core package — the
// same package real copr's own REQUIREMENTS already lists), exactly as
// this batch's own assignment brief directs. This is a real behavioral
// difference, documented rather than hidden: the `dnf copr` plugin
// always talks to whatever copr frontend IT is configured for, so the
// `host`/`protocol` arguments here are used only to compute the
// repository's expected .repo filename (for idempotency and this
// module's own `repo`/`repo_filename` return values, matching real
// copr's own naming: `_copr:<host>:<user>:<project>.repo`, with a
// leading "@group" name sanitized to "group_<name>" exactly as real
// copr's own _sanitize_username does) — they are NOT passed to `dnf
// copr` itself, which has no per-invocation host override in the
// classic dnf-plugins-core CLI.
//
// Args: name (string, required) — "<user-or-@group>/<project>"; state
// (enabled|disabled|absent, default "enabled"); chroot (string,
// optional) — passed through to `dnf copr enable`; host (string,
// default "copr.fedorainfracloud.org"); protocol (string, default
// "https") — accepted for argspec compatibility with real copr, unused
// here (see above); includepkgs/excludepkgs ([]string, optional) — real
// copr writes these into the repo file's own content at enable time;
// this port does the same with a best-effort `sed`/append AFTER a fresh
// `dnf copr enable` actually ran (i.e. only when this task itself
// caused the repo file to be (re)created) — it does NOT detect an
// includepkgs/excludepkgs-only change against an already-enabled repo
// the way real copr's own content comparison would, a documented gap.
//
// State semantics: enabled — runs `dnf copr enable` unless the computed
// repo file already reports `enabled=1`; disabled — runs `dnf copr
// enable` first if the repo file doesn't exist yet at all (matching
// real copr's own `_disable_repo`, which also implicitly enables an
// unknown repo before it can mark it disabled), then `dnf copr disable`
// unless already `enabled=0`; absent — runs `dnf copr remove` unless
// the repo file is already absent.
func moduleCopr(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 {
		return Result{}, errArg("copr: name must be of the form \"user-or-@group/project\", got %q", name)
	}
	user := coprSanitizeUsername(parts[0])
	project := parts[1]
	state := argString(args, "state", "enabled")
	chroot := argString(args, "chroot", "")
	host := argString(args, "host", "copr.fedorainfracloud.org")
	includepkgs := argStringList(args, "includepkgs")
	excludepkgs := argStringList(args, "excludepkgs")

	repoFilename := fmt.Sprintf("_copr:%s:%s:%s.repo", host, user, project)
	repoPath := "/etc/yum.repos.d/" + repoFilename
	repo := fmt.Sprintf("%s/%s/%s", host, user, project)

	exists, err := pathExists(ctx, conn, repoPath)
	if err != nil {
		return Result{}, err
	}
	enabled := false
	if exists {
		res, err := runStatus(ctx, conn, "grep -q '^enabled=1' "+shellQuote(repoPath))
		if err != nil {
			return Result{}, err
		}
		enabled = res.RC == 0
	}

	switch state {
	case "enabled":
		if exists && enabled {
			return Ok("enabled").WithExtra("repo", repo).WithExtra("repo_filename", repoFilename), nil
		}
		if err := coprEnable(ctx, conn, name, chroot); err != nil {
			return Result{}, err
		}
		if err := coprApplyPkgFilters(ctx, conn, repoPath, includepkgs, excludepkgs); err != nil {
			return Result{}, err
		}
		return Changed("enabled").WithExtra("repo", repo).WithExtra("repo_filename", repoFilename), nil

	case "disabled":
		if exists && !enabled {
			return Ok("disabled").WithExtra("repo", repo).WithExtra("repo_filename", repoFilename), nil
		}
		if !exists {
			if err := coprEnable(ctx, conn, name, chroot); err != nil {
				return Result{}, err
			}
		}
		if _, err := run(ctx, conn, "dnf -y copr disable "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed("disabled").WithExtra("repo", repo).WithExtra("repo_filename", repoFilename), nil

	case "absent":
		if !exists {
			return Ok("absent").WithExtra("repo", repo).WithExtra("repo_filename", repoFilename), nil
		}
		if _, err := run(ctx, conn, "dnf -y copr remove "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed("absent").WithExtra("repo", repo).WithExtra("repo_filename", repoFilename), nil

	default:
		return Result{}, errArg("copr: state must be enabled, disabled, or absent, got %q", state)
	}
}

func coprEnable(ctx context.Context, conn remoteexec.Connection, name, chroot string) error {
	cmd := "dnf -y copr enable " + shellQuote(name)
	if chroot != "" {
		cmd += " " + shellQuote(chroot)
	}
	_, err := run(ctx, conn, cmd)
	return err
}

// coprApplyPkgFilters best-effort-appends includepkgs/excludepkgs lines
// to a freshly-(re)enabled repo file, skipping a key already present.
func coprApplyPkgFilters(ctx context.Context, conn remoteexec.Connection, repoPath string, includepkgs, excludepkgs []string) error {
	if len(includepkgs) > 0 {
		cmd := "grep -q '^includepkgs=' " + shellQuote(repoPath) +
			" || printf 'includepkgs=%s\\n' " + shellQuote(strings.Join(includepkgs, ",")) + " >> " + shellQuote(repoPath)
		if _, err := run(ctx, conn, cmd); err != nil {
			return err
		}
	}
	if len(excludepkgs) > 0 {
		cmd := "grep -q '^excludepkgs=' " + shellQuote(repoPath) +
			" || printf 'excludepkgs=%s\\n' " + shellQuote(strings.Join(excludepkgs, ",")) + " >> " + shellQuote(repoPath)
		if _, err := run(ctx, conn, cmd); err != nil {
			return err
		}
	}
	return nil
}

// coprSanitizeUsername mirrors real copr's own _sanitize_username: a
// leading "@" (a Copr group) becomes "group_" instead.
func coprSanitizeUsername(user string) string {
	if strings.HasPrefix(user, "@") {
		return "group_" + user[1:]
	}
	return user
}
