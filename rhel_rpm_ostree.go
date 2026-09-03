package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRhelRpmOstree implements (a subset of) Ansible's
// `rhel_rpm_ostree` module: a `package`-module-compatible way to
// install/remove packages via `rpm-ostree` on RHEL for Edge/rpm-ostree
// based systems (dispatched to by name when rhel_facts.go has set
// pkg_mgr, matching real rhel_rpm_ostree's own stated purpose).
//
// Args: name ([]string or string, required, aliased from pkg); state
// (present|installed|latest|absent|removed — default is effectively
// "present", matching real rhel_rpm_ostree's own documented default
// behavior of "present unless autoremove is set", a real
// rhel_rpm_ostree option this port does not implement — see below).
//
// Package presence is checked via `rpm -q` (the RPM database still
// exists and is queryable on an rpm-ostree system, the same technique
// dnf.go/dnf5.go already use for their own package managers).
//
// Real rhel_rpm_ostree also documents an `autoremove` option (implying
// absent when set, with no explicit state) that this port does not
// implement — `state` must be given explicitly here. state=latest is
// treated the same as state=present (always re-runs `rpm-ostree
// install` and reports changed, since rpm-ostree has no meaningful
// per-package "is this the latest version" query the way dnf/apt do;
// use rpm_ostree_upgrade.go for a real whole-system upgrade instead) —
// a real narrowing versus real rhel_rpm_ostree's own "latest" handling,
// documented rather than silently claimed.
//
// Like rpm_ostree_upgrade.go, every rpm-ostree transaction here is
// staged, not live: the running system is unaffected until the next
// reboot (rpm-ostree's whole transactional-image model) — this port
// reports Changed as soon as the transaction is queued, the same as
// real rhel_rpm_ostree does, but neither can make the change visible to
// commands run later in the SAME connection without a reboot in
// between (the same limitation reboot.go documents for its own
// can't-wait-for-reconnect gap).
func moduleRhelRpmOstree(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		if s, err := requireString(args, "name"); err == nil {
			names = []string{s}
		} else {
			return Result{}, errArg("rhel_rpm_ostree: missing required argument: name")
		}
	}
	state := argString(args, "state", "present")

	switch state {
	case "absent", "removed":
		var toRemove []string
		for _, name := range names {
			installed, err := rpmInstalled(ctx, conn, name)
			if err != nil {
				return Result{}, err
			}
			if installed {
				toRemove = append(toRemove, name)
			}
		}
		if len(toRemove) == 0 {
			return Ok("No changes made."), nil
		}
		if _, err := run(ctx, conn, "rpm-ostree uninstall "+quoteAll(toRemove)); err != nil {
			return Result{}, err
		}
		return Changed("Queued uninstall of " + strings.Join(toRemove, ", ") + "; a reboot is required for it to take effect."), nil

	case "present", "installed", "latest":
		toInstall := names
		if state != "latest" {
			toInstall = nil
			for _, name := range names {
				installed, err := rpmInstalled(ctx, conn, name)
				if err != nil {
					return Result{}, err
				}
				if !installed {
					toInstall = append(toInstall, name)
				}
			}
		}
		if len(toInstall) == 0 {
			return Ok("No changes made."), nil
		}
		if _, err := run(ctx, conn, "rpm-ostree install "+quoteAll(toInstall)); err != nil {
			return Result{}, err
		}
		return Changed("Queued install of " + strings.Join(toInstall, ", ") + "; a reboot is required for it to take effect."), nil

	default:
		return Result{}, errArg("rhel_rpm_ostree: state must be present, installed, latest, absent, or removed, got %q", state)
	}
}
