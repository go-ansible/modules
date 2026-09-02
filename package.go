package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePackage implements Ansible's OS-agnostic `package` module:
// detects the target's package manager and delegates to the matching
// module already implemented in this package.
//
// Args: name (string or []string, required); state (string, required —
// passed through to the delegate as-is, so its accepted values are
// whatever the resolved delegate accepts).
//
// Detection order: apt-get, then dnf, then yum (treated as a dnf-family
// alias — real Ansible documents that on a modern dnf-based system, yum
// is itself just dnf under a symlink/shim). Real ansible.builtin.package
// additionally supports zypper (SUSE), pacman (Arch), apk (Alpine), and
// more via ansible_facts.pkg_mgr; this port covers only the
// apt/dnf/yum-as-dnf-alias families, since those are the only
// package-manager modules implemented in this package. An unrecognized
// target fails cleanly rather than silently no-op'ing.
//
// Unlike real package (which resolves and re-executes the target module
// as its own Ansible task, going back through the plugin/connection
// layers), this port calls the delegate's Go function directly —
// cheaper and simpler, and observably identical since both paths end up
// running the same target-side commands.
func modulePackage(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	mgr, err := detectPackageManager(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	switch mgr {
	case "apt":
		return moduleApt(ctx, conn, args)
	case "dnf":
		return moduleDnf(ctx, conn, args)
	default:
		return Fail("package: no supported package manager found (looked for apt-get, dnf, yum)"), nil
	}
}

// detectPackageManager probes the target, in priority order, for
// apt-get, dnf, then yum (a dnf-family alias) via one combined shell
// command, returning "apt", "dnf", or "" if none was found.
func detectPackageManager(ctx context.Context, conn remoteexec.Connection) (string, error) {
	const probe = "if command -v apt-get >/dev/null 2>&1; then echo apt; " +
		"elif command -v dnf >/dev/null 2>&1; then echo dnf; " +
		"elif command -v yum >/dev/null 2>&1; then echo dnf; " +
		"else echo none; fi"
	out, err := run(ctx, conn, probe)
	if err != nil {
		return "", err
	}
	if out == "none" {
		return "", nil
	}
	return out, nil
}
