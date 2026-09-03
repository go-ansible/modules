package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRpmOstreeUpgrade implements Ansible's `rpm_ostree_upgrade`
// module: runs `rpm-ostree upgrade` to pull and stage the latest
// available tree for the current rpm-ostree deployment.
//
// Args: allow_downgrade (bool, default false) — adds
// `--allow-downgrade`; cache_only (bool, default false) — adds
// `--cache-only`; os (string, optional) — adds `--os=<val>`; peer
// (bool, default false) — adds `--peer`.
//
// Changed is determined by checking rpm-ostree's own stdout for "No
// upgrade available." (its documented message when the tree is already
// current) rather than by any separate idempotency probe — matching
// rpm-ostree's own transactional model, where "upgrade" is always safe
// to (re-)run and simply reports whether it did anything.
//
// Like rhel_rpm_ostree.go, an upgrade staged here does NOT take effect
// on the running system until the next reboot — rpm-ostree's whole
// transactional-image model, the same limitation documented there and
// (for an unrelated reason) in reboot.go.
func moduleRpmOstreeUpgrade(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	var b strings.Builder
	b.WriteString("rpm-ostree upgrade")
	if argBool(args, "allow_downgrade", false) {
		b.WriteString(" --allow-downgrade")
	}
	if argBool(args, "cache_only", false) {
		b.WriteString(" --cache-only")
	}
	if os := argString(args, "os", ""); os != "" {
		b.WriteString(" --os=" + shellQuote(os))
	}
	if argBool(args, "peer", false) {
		b.WriteString(" --peer")
	}

	res, err := runStatus(ctx, conn, b.String())
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("rpm_ostree_upgrade: " + strings.TrimSpace(res.Stderr)), nil
	}
	msg := strings.TrimSpace(res.Stdout)
	if strings.Contains(msg, "No upgrade available") {
		return Ok(msg), nil
	}
	return Changed(msg + " A reboot is required for the upgrade to take effect."), nil
}
