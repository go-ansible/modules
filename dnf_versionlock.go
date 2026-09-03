package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDnfVersionlock implements (a subset of) Ansible's
// `dnf_versionlock` module: locks or excludes package versions via the
// `dnf` `versionlock` plugin.
//
// Args: name (string or []string, default []) — a package-name-spec (in
// the format `dnf repoquery` accepts, e.g. `nginx`, `bind-32:9.11*`), or
// list of specs; mutually exclusive with state=clean; raw (bool,
// default false) — pass specs through to `dnf versionlock` as-is via
// `--raw`, without this port's own idempotency substring check trying
// to expand them; state (present|excluded|absent|clean, default
// "present") — present adds a spec to the locklist (pinning it);
// excluded adds it as an *excluded* entry (packages matching it are
// excluded from transactions entirely, written to the locklist as
// `!spec`); absent removes locklist entries matching a spec; clean
// removes every entry, and is mutually exclusive with name.
//
// Simplifications vs real dnf_versionlock: real dnf_versionlock.py
// resolves each name spec against `dnf repoquery` to NEVRA-expand it
// into one full locklist entry per matching (available or installed)
// package version, then compares those expanded entries against the
// current locklist for idempotency, and works around two known dnf
// versionlock plugin bugs by issuing one `dnf versionlock add/exclude`
// invocation per spec rather than a single multi-spec call. This port
// does the "one invocation per spec" part (there's no reason not to,
// and it sidesteps the same plugin bugs), but not the repoquery
// expansion: idempotency is checked by a plain substring search over
// `dnf -q versionlock list`'s current output instead (does entry
// already contain this spec, or "!"+spec for excluded/checking
// absent), which is weaker than real NEVRA-aware matching but avoids
// depending on repoquery's own package-database access. No dnf5
// (`dnf5 versionlock list`'s different stanza-based output format) is
// parsed — only the classic dnf4 versionlock plugin's one-line-per-entry
// format.
func moduleDnfVersionlock(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	raw := argBool(args, "raw", false)
	state := argString(args, "state", "present")
	switch state {
	case "present", "excluded", "absent", "clean":
	default:
		return Result{}, errArg("dnf_versionlock: state must be present, excluded, absent, or clean, got %q", state)
	}
	if state == "clean" && len(names) > 0 {
		return Result{}, errArg("dnf_versionlock: state=clean is mutually exclusive with name")
	}
	if state != "clean" && len(names) == 0 {
		return Result{}, errArg("dnf_versionlock: name is required for state=%s", state)
	}

	locklistPre, err := dnfVersionlockList(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	rawFlag := ""
	if raw {
		rawFlag = " --raw"
	}

	var toAdd, toDelete []string
	switch state {
	case "present", "excluded":
		marker := ""
		if state == "excluded" {
			marker = "!"
		}
		for _, p := range names {
			if !dnfLocklistContains(locklistPre, marker+p) {
				toAdd = append(toAdd, p)
			}
		}
		cmd := "add"
		if state == "excluded" {
			cmd = "exclude"
		}
		for _, p := range toAdd {
			if _, err := run(ctx, conn, "dnf -q versionlock "+cmd+rawFlag+" "+shellQuote(p)); err != nil {
				return Result{}, err
			}
		}

	case "absent":
		for _, p := range names {
			if dnfLocklistContains(locklistPre, p) {
				toDelete = append(toDelete, p)
			}
		}
		for _, p := range toDelete {
			if _, err := run(ctx, conn, "dnf -q versionlock delete"+rawFlag+" "+shellQuote(p)); err != nil {
				return Result{}, err
			}
		}

	case "clean":
		toDelete = locklistPre
		if len(toDelete) > 0 {
			if _, err := run(ctx, conn, "dnf -q versionlock clear"); err != nil {
				return Result{}, err
			}
		}
	}

	changed := len(toAdd) > 0 || len(toDelete) > 0
	locklistPost := locklistPre
	if changed {
		locklistPost, err = dnfVersionlockList(ctx, conn)
		if err != nil {
			return Result{}, err
		}
	}

	res := Result{Changed: changed}
	res = res.WithExtra("locklist_pre", locklistPre)
	res = res.WithExtra("locklist_post", locklistPost)
	res = res.WithExtra("specs_toadd", toAdd)
	res = res.WithExtra("specs_todelete", toDelete)
	return res, nil
}

// dnfVersionlockList runs `dnf -q versionlock list` and returns its
// non-empty, whitespace-trimmed lines (each already-locklisted entry,
// e.g. "bash-0:4.4.20-1.el8_4.*" or, for an excluded entry,
// "!bind-32:9.11.26-4.el8_4.*").
func dnfVersionlockList(ctx context.Context, conn remoteexec.Connection) ([]string, error) {
	out, err := run(ctx, conn, "dnf -q versionlock list")
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Fields(out) {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// dnfLocklistContains reports whether any entry in locklist contains
// spec as a substring (see the doc comment on moduleDnfVersionlock for
// why this is a substring check rather than real NEVRA matching).
func dnfLocklistContains(locklist []string, spec string) bool {
	for _, entry := range locklist {
		if strings.Contains(entry, spec) {
			return true
		}
	}
	return false
}
