package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRpmOstreePkg implements Ansible's `rpm_ostree_pkg` module
// (community.general): layers or removes an INDIVIDUAL overlay package
// on an rpm-ostree system, via `rpm-ostree install`/`rpm-ostree
// uninstall` — distinct from rpm_ostree_upgrade.go (a whole-system
// upgrade) and rhel_rpm_ostree.go (this port's own broader
// package-module-compatible install/remove).
//
// Args: name ([]string, required, alias pkg) — one or more overlay
// package names; state (present|absent, default "present"); apply_live
// (bool, default false) — adds `--apply-live --assumeyes` when
// state=present (ignored for state=absent), matching real
// rpm_ostree_pkg.py exactly.
//
// Every invocation always appends `--allow-inactive --idempotent
// --unchanged-exit-77` — matching real rpm_ostree_pkg.py's own command
// construction exactly, including its reliance on rpm-ostree's own
// idempotent-install support: rc==0 means the transaction made a
// change (Changed); rc==77 (rpm-ostree's own documented
// "--unchanged-exit-77" sentinel) means nothing needed to change
// (Ok, not a Fail — this is the module's OWN idempotency signal, read
// directly from the child process's exit code rather than probed for
// beforehand); any other rc is a Fail. `needs_reboot` is set from
// whether rpm-ostree's own stdout contains 'Changes queued for next
// boot. Run "systemctl reboot" to start a reboot' — matching real
// rpm_ostree_pkg.py's own return field of the same name and meaning
// exactly. Real rpm_ostree_pkg.py declares check_mode support: none
// (not full or partial) — this port does not attempt to fake one
// either; every call actually runs rpm-ostree.
func moduleRpmOstreePkg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		names = argStringList(args, "pkg")
	}
	if len(names) == 0 {
		return Result{}, errArg("rpm_ostree_pkg: missing required argument: name (or its alias pkg)")
	}
	state := argString(args, "state", "present")
	applyLive := argBool(args, "apply_live", false)

	action := "install"
	if state == "absent" {
		action = "uninstall"
	} else if state != "present" {
		return Result{}, errArg("rpm_ostree_pkg: state must be present or absent, got %q", state)
	}

	var b strings.Builder
	b.WriteString("rpm-ostree ")
	b.WriteString(action)
	if applyLive && state == "present" {
		b.WriteString(" --apply-live --assumeyes")
	}
	b.WriteString(" --allow-inactive --idempotent --unchanged-exit-77")
	for _, n := range names {
		b.WriteString(" " + shellQuote(n))
	}
	cmdLine := b.String()

	res, err := runStatus(ctx, conn, cmdLine)
	if err != nil {
		return Result{}, err
	}
	needsReboot := strings.Contains(res.Stdout, `Changes queued for next boot. Run "systemctl reboot" to start a reboot`)

	var result Result
	switch res.RC {
	case 0:
		result = Changed(strings.TrimSpace(res.Stdout))
	case 77:
		result = Ok(strings.TrimSpace(res.Stdout))
	default:
		return Fail("rpm_ostree_pkg: " + strings.TrimSpace(res.Stderr)), nil
	}
	result = result.WithExtra("action", action)
	result = result.WithExtra("packages", names)
	result = result.WithExtra("cmd", cmdLine)
	result = result.WithExtra("needs_reboot", needsReboot)
	return result, nil
}
