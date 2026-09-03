package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGitConfig implements (a subset of) Ansible's `git_config` module:
// manages one git configuration key entirely via `git config` itself —
// mirroring sysctl.go's "manage through the tool, not by hand-editing
// the backing file" model. Every read and write here goes through `git
// config --get`/`--get-all`/`--replace-all`/`--add`/`--unset-all`,
// never by directly editing .gitconfig or a repo's .git/config.
//
// Args: name (string, required); value (string, required when
// state=present — community.general 11.0.0 tightened real git_config
// to require this whenever state=present; see below for why this port
// has no read-only mode to fall back to); scope (file|local|global|
// system, default "system", matching real git_config's own documented
// default); repo (string, path, required when scope=local — passed as
// `git -C <repo> config --local ...`); file (string, path, required
// when scope=file — passed as `git config --file <file> ...`); state
// (present|absent, default "present"); add_mode (add|replace-all,
// default "replace-all") — "replace-all" collapses the key down to
// exactly one value via `git config --replace-all`; "add" appends a
// value alongside any existing ones via `git config --add`, matching
// real git_config's own two documented add_mode semantics.
//
// Simplifications vs real git_config: no read-only mode (real
// git_config lets `value` be omitted entirely to just report the
// current value with changed=false; every module here is a
// changed/failed/msg action, not a query, so that use case is out of
// scope — a caller wanting to just read a value should reach for a
// git_config_info-equivalent instead); idempotency for the default
// add_mode=replace-all is checked via a single `git config --get` (git
// itself returns the LAST-set value for a multi-valued key), not a full
// multi-value comparison, so a key that already holds several
// conflicting values is collapsed to one on its first managed run
// rather than being recognized as "the wanted value is already among
// them" — real git_config's own --replace-all does the same collapsing,
// this port just doesn't special-case detecting it was already
// collapsed; for add_mode=add, idempotency is checked via `git config
// --get-all` and an exact-line match against `value`, so a value that
// already exists verbatim is not re-added, matching real git_config's
// own documented add-mode idempotency. No `add_mode` value beyond the
// two real ones is accepted.
func moduleGitConfig(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	scope := argString(args, "scope", "system")
	if scope != "file" && scope != "local" && scope != "global" && scope != "system" {
		return Result{}, errArg("git_config: scope must be file, local, global, or system, got %q", scope)
	}
	repo := argString(args, "repo", "")
	file := argString(args, "file", "")
	if scope == "local" && repo == "" {
		return Result{}, errArg("git_config: repo is required when scope is local")
	}
	if scope == "file" && file == "" {
		return Result{}, errArg("git_config: file is required when scope is file")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("git_config: state must be present or absent, got %q", state)
	}
	addMode := argString(args, "add_mode", "replace-all")
	if addMode != "add" && addMode != "replace-all" {
		return Result{}, errArg("git_config: add_mode must be add or replace-all, got %q", addMode)
	}

	base := gitConfigBaseCmd(scope, repo, file)

	if state == "absent" {
		res, err := runStatus(ctx, conn, base+" --get "+shellQuote(name)+" 2>/dev/null")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, base+" --unset-all "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " unset"), nil
	}

	value, err := requireString(args, "value")
	if err != nil {
		return Result{}, errArg("git_config: value is required when state is present")
	}

	if addMode == "add" {
		res, err := runStatus(ctx, conn, base+" --get-all "+shellQuote(name)+" 2>/dev/null")
		if err != nil {
			return Result{}, err
		}
		if res.RC == 0 {
			for _, l := range splitLines(res.Stdout) {
				if l == value {
					return Ok(name + " already has value " + value), nil
				}
			}
		}
		if _, err := run(ctx, conn, base+" --add "+shellQuote(name)+" "+shellQuote(value)); err != nil {
			return Result{}, err
		}
		return Changed(name + " added " + value), nil
	}

	res, err := runStatus(ctx, conn, base+" --get "+shellQuote(name)+" 2>/dev/null")
	if err != nil {
		return Result{}, err
	}
	if res.RC == 0 && strings.TrimSpace(res.Stdout) == value {
		return Ok(name + " already set to " + value), nil
	}
	if _, err := run(ctx, conn, base+" --replace-all "+shellQuote(name)+" "+shellQuote(value)); err != nil {
		return Result{}, err
	}
	return Changed(name + " set to " + value), nil
}

// gitConfigBaseCmd builds the "git [-C repo] config <scope-flag>"
// prefix shared by every git config invocation moduleGitConfig makes,
// so its shape can be exercised directly in tests via fakeConn's exact
// command matching.
func gitConfigBaseCmd(scope, repo, file string) string {
	var b strings.Builder
	b.WriteString("git")
	if scope == "local" {
		b.WriteString(" -C ")
		b.WriteString(shellQuote(repo))
	}
	b.WriteString(" config")
	switch scope {
	case "system":
		b.WriteString(" --system")
	case "global":
		b.WriteString(" --global")
	case "local":
		b.WriteString(" --local")
	case "file":
		b.WriteString(" --file ")
		b.WriteString(shellQuote(file))
	}
	return b.String()
}
