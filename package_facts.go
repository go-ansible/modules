package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePackageFacts implements (a subset of) Ansible's `package_facts`
// module: gathers the list of installed packages into
// Extra["packages"], shaped like real Ansible's ansible_facts.packages —
// a map from package name to a list of entries (one per installed
// version), each entry a map with at least "version".
//
// Args: manager (string, default "auto" — "auto" triggers detection
// (see detectPackageManager); "apt" forces the dpkg-query path; any of
// "dnf", "dnf5", "yum", "rpm" force the rpm -qa path; anything else
// fails cleanly).
//
// Real package_facts additionally supports apk (Alpine), pacman
// (Arch), FreeBSD pkg, OpenBSD pkg_info, and portage — none of those
// are implemented here, matching modulePackage's own apt/dnf-family-only
// coverage. Real package_facts' entries also carry "release", "epoch",
// "arch", and "source"; this port only fills "version" (dpkg/rpm's own
// combined version-release string for rpm, or dpkg's version field for
// apt), since that's the only field cheaply available from a
// single-line dpkg-query/rpm -qa format string without an additional
// per-package query.
func modulePackageFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	manager := argString(args, "manager", "auto")

	var mgr string
	switch manager {
	case "auto":
		detected, err := detectPackageManager(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		mgr = detected
	case "apt":
		mgr = "apt"
	case "dnf", "dnf5", "yum", "rpm":
		mgr = "dnf"
	default:
		return Result{}, errArg("package_facts: unsupported manager %q (this port supports auto, apt, dnf, dnf5, yum, rpm)", manager)
	}

	var cmd string
	switch mgr {
	case "apt":
		cmd = `dpkg-query -W -f='${Package} ${Version}\n'`
	case "dnf":
		cmd = `rpm -qa --qf '%{NAME} %{VERSION}-%{RELEASE}\n'`
	default:
		return Fail("package_facts: no supported package manager found (looked for apt-get, dnf, yum)"), nil
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("package_facts: " + strings.TrimSpace(res.Stderr)), nil
	}

	packages := map[string]any{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		name, version := fields[0], fields[1]
		existing, _ := packages[name].([]map[string]any)
		packages[name] = append(existing, map[string]any{"version": version})
	}
	return Ok(fmt.Sprintf("gathered %d package names", len(packages))).WithExtra("packages", packages), nil
}
