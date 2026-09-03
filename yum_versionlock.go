package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleYumVersionlock implements (a subset of) Ansible's
// `yum_versionlock` module: adds or removes packages from yum's
// `versionlock` list (via the yum-plugin-versionlock plugin), preventing
// them from being updated.
//
// Args: name (string or []string, required) — a package name or
// name-with-version/wildcard spec (e.g. `httpd`, `httpd-0:2.4.57-2.el9`);
// state (present|absent, default "present").
//
// Simplifications vs real yum_versionlock: real yum_versionlock.py
// parses each `yum versionlock list` entry's NEVRA (trying both yum's
// own `epoch:name-version-release.arch` shape and dnf's
// `name-epoch:version-release.arch` shape, since on DNF-based distros
// `yum` is a symlink to `dnf`) and fnmatch-compares the extracted name
// against each requested spec. This port instead checks idempotency by
// a plain substring search over `yum versionlock list`'s raw output —
// weaker (it can't, for instance, tell a version-qualified spec like
// `httpd-0:2.4.57-2.el9` apart from a bare `httpd` entry the way real
// NEVRA parsing does), but avoids depending on that regex/fnmatch
// machinery. Like real yum_versionlock.py, all requested names needing
// a change are added/removed in a single `yum -q versionlock
// add/delete` invocation rather than one call per name.
func moduleYumVersionlock(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		return Result{}, errArg("yum_versionlock: name is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("yum_versionlock: state must be present or absent, got %q", state)
	}

	out, err := run(ctx, conn, "yum versionlock list")
	if err != nil {
		return Result{}, err
	}
	locklist := strings.Fields(out)

	var toChange []string
	for _, n := range names {
		present := yumLocklistContains(locklist, n)
		if state == "present" && !present {
			toChange = append(toChange, n)
		} else if state == "absent" && present {
			toChange = append(toChange, n)
		}
	}

	if len(toChange) > 0 {
		cmd := "add"
		if state == "absent" {
			cmd = "delete"
		}
		if _, err := run(ctx, conn, "yum -q versionlock "+cmd+" "+quoteAll(toChange)); err != nil {
			return Result{}, err
		}
	}

	res := Result{Changed: len(toChange) > 0}
	res = res.WithExtra("packages", names)
	res = res.WithExtra("state", state)
	return res, nil
}

// yumLocklistContains reports whether any entry in locklist contains
// name as a substring (see the doc comment on moduleYumVersionlock for
// why this is a substring check rather than real NEVRA-name matching).
func yumLocklistContains(locklist []string, name string) bool {
	for _, entry := range locklist {
		if strings.Contains(entry, name) {
			return true
		}
	}
	return false
}
