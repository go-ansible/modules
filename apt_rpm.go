package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAptRpm implements (a subset of) Ansible's `apt_rpm` module:
// package management via the ALT Linux `apt-rpm` toolchain — `apt-get`
// (the high-level manager) driving `rpm` (the low-level one), an
// obscure, mostly historical combination distinct from Debian/Ubuntu's
// own dpkg-backed `apt` (apt.go's own `apt` module).
//
// Args: package ([]string, aliases name/pkg) — package names, or
// local `.rpm` file paths for state=present/installed (resolved to a
// package name via `rpm -qp --queryformat %{NAME}`, a CLI-only
// equivalent of real apt_rpm's own optional Python `rpm` bindings
// dependency — see Simplifications below). state (absent|present|
// present_not_latest|installed|removed|latest, default "present") —
// present/installed/present_not_latest install-if-missing without
// forcing an upgrade; latest allows upgrading an already-installed
// package (matching real apt_rpm's own community.general 11.0.0
// semantics change, documented in its own OPTIONS: before that
// release present/installed meant what latest means now).
// update_cache (bool, default false) — `apt-get update` first.
// clean (bool, default false) — `apt-get clean`, reported changed only
// if /var/cache/apt/archives's own total size actually shrank,
// matching real apt_rpm's own before/after `dir_size()` comparison.
// dist_upgrade (bool, default false) — `apt-get -y dist-upgrade`.
// update_kernel (bool, default false) — `/usr/sbin/update-kernel -y`.
//
// Simplifications vs real apt_rpm: real apt_rpm's own package-presence
// probe (`query_package_provides`) additionally supports comparing a
// LOCAL rpm file's version against an installed package's version via
// Python's `rpm` bindings (`rpm.versionCompare`) — this port has no Go
// rpm-header parser and instead shells out to `rpm -qp`/`rpm -q` for
// everything, including resolving a local file's package name (see
// Args above); a state=latest upgrade check against apt-cache policy
// output is still replicated (see aptRpmProvides), matching real
// apt_rpm's own plain lexicographic `installed >= candidate` string
// comparison — a real, documented quirk of real apt_rpm's own
// check_package_version, not a general version comparator, reproduced
// here exactly rather than "fixed" to be more correct than real
// apt_rpm itself is.
//
// Real apt_rpm's own update_kernel path has a real, surprising
// inversion worth calling out explicitly rather than silently copying:
// its own `update_kernel()` returns changed = (UPDATE_KERNEL_ZERO NOT
// IN out), where UPDATE_KERNEL_ZERO is the string "\nTry to install new
// kernel " — meaning real apt_rpm reports changed=true precisely when
// update-kernel's own output does NOT mention trying to install a new
// kernel. This port replicates that exact (counter-intuitive-looking)
// condition verbatim rather than "fixing" it to look more sensible,
// per this project's rule to document a real deviation honestly rather
// than paper over it, and this is not a deviation — it is what real
// apt_rpm's own source does.
//
// Real apt_rpm has NO check_mode support at all (matching this port's
// blanket lack of check_mode support everywhere — a runtime-engine
// concern outside every module's own Func signature here).
func moduleAptRpm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	packages := argStringList(args, "package")
	if len(packages) == 0 {
		packages = argStringList(args, "name")
	}
	if len(packages) == 0 {
		packages = argStringList(args, "pkg")
	}
	state := argString(args, "state", "present")
	switch state {
	case "absent", "present", "present_not_latest", "installed", "removed", "latest":
	default:
		return Result{}, errArg("apt_rpm: state must be one of absent, present, present_not_latest, installed, removed, latest, got %q", state)
	}
	updateCache := argBool(args, "update_cache", false)
	clean := argBool(args, "clean", false)
	distUpgrade := argBool(args, "dist_upgrade", false)
	updateKernel := argBool(args, "update_kernel", false)

	for _, bin := range []string{"/usr/bin/apt-get", "/usr/bin/rpm"} {
		exists, err := pathExists(ctx, conn, bin)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail("apt_rpm: cannot find /usr/bin/apt-get and/or /usr/bin/rpm"), nil
		}
	}

	modified := false
	var outputs []string

	if updateCache {
		if _, err := run(ctx, conn, "env LANGUAGE=C LC_ALL=C apt-get update"); err != nil {
			return Result{}, err
		}
	}

	if clean {
		before, err := aptRpmCacheSize(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if _, err := run(ctx, conn, "apt-get clean"); err != nil {
			return Result{}, err
		}
		after, err := aptRpmCacheSize(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if before != after {
			modified = true
		}
	}

	if distUpgrade {
		out, err := run(ctx, conn, "env LANGUAGE=C LC_ALL=C apt-get -y dist-upgrade")
		if err != nil {
			return Result{}, err
		}
		if !strings.Contains(out, "0 upgraded, 0 newly installed") {
			modified = true
		}
		outputs = append(outputs, out)
	}

	if updateKernel {
		res, err := runStatus(ctx, conn, "env LANGUAGE=C LC_ALL=C /usr/sbin/update-kernel -y")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			if strings.Contains(res.Stderr, "There are no available kernels") {
				outputs = append(outputs, res.Stdout)
			} else {
				return Fail(fmt.Sprintf("apt_rpm: error while updating kernel: %s", firstNonEmpty(res.Stderr, res.Stdout))), nil
			}
		} else {
			if !strings.Contains(res.Stdout, "Try to install new kernel ") {
				modified = true
			}
			outputs = append(outputs, res.Stdout)
		}
	}

	switch state {
	case "installed", "present", "present_not_latest", "latest":
		changed, msg, failMsg, err := aptRpmInstall(ctx, conn, packages, state == "latest")
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		if changed {
			modified = true
		}
		outputs = append(outputs, msg)
	case "absent", "removed":
		changed, msg, err := aptRpmRemove(ctx, conn, packages)
		if err != nil {
			return Result{}, err
		}
		if changed {
			modified = true
		}
		outputs = append(outputs, msg)
	}

	msg := strings.Join(outputs, "")
	result := Ok(msg)
	if modified {
		result = Changed(msg)
	}
	return result, nil
}

// aptRpmCacheSize returns the total byte size of
// /var/cache/apt/archives, matching real apt_rpm's own dir_size()
// before/after comparison for `clean`'s changed status.
func aptRpmCacheSize(ctx context.Context, conn remoteexec.Connection) (string, error) {
	out, err := run(ctx, conn, "du -sb /var/cache/apt/archives/ 2>/dev/null | cut -f1")
	if err != nil {
		return "", err
	}
	if out == "" {
		return "0", nil
	}
	return out, nil
}

// aptRpmPackageName resolves a local .rpm file's own package name via
// `rpm -qp`, this port's CLI-only substitute for real apt_rpm's
// optional Python `rpm` bindings dependency (see moduleAptRpm's own
// doc comment).
func aptRpmPackageName(ctx context.Context, conn remoteexec.Connection, path string) (string, error) {
	return run(ctx, conn, "rpm -qp --queryformat "+shellQuote("%{NAME}")+" "+shellQuote(path))
}

// aptRpmProvides reports whether name (or, for a local .rpm path, the
// package it names) is currently provided, matching real apt_rpm's own
// query_package_provides: `rpm -q --provides`. When allowUpgrade is
// true, a provided package is additionally checked against `apt-cache
// policy` output — real apt_rpm's own check_package_version, a plain
// lexicographic string comparison of the Installed/Candidate version
// fields, NOT a real version comparator (see moduleAptRpm's own doc
// comment on why this is reproduced as-is).
func aptRpmProvides(ctx context.Context, conn remoteexec.Connection, name string, allowUpgrade bool) (bool, error) {
	pkgName := name
	if strings.HasSuffix(name, ".rpm") {
		resolved, err := aptRpmPackageName(ctx, conn, name)
		if err != nil {
			return false, fmt.Errorf("apt_rpm: failed to read package name from local RPM file %s: %w", name, err)
		}
		pkgName = resolved
	}
	res, err := runStatus(ctx, conn, "rpm -q --provides "+shellQuote(pkgName)+" >/dev/null 2>&1")
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, nil
	}
	if !allowUpgrade {
		return true, nil
	}
	policy, err := run(ctx, conn, "env LANGUAGE=C LC_ALL=C apt-cache policy "+shellQuote(pkgName))
	if err != nil {
		return false, err
	}
	installed, candidate := aptRpmParsePolicy(policy)
	return installed >= candidate, nil
}

// aptRpmParsePolicy extracts the "Installed:"/"Candidate:" version
// strings from `apt-cache policy <pkg>` output — a line-oriented
// substitute for real apt_rpm's own fragile positional
// `re.split("\n |: ", out)[2]`/`[4]` slicing of the same output, which
// both approaches read the same two fields from.
func aptRpmParsePolicy(out string) (installed, candidate string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Installed:"):
			installed = strings.TrimSpace(strings.TrimPrefix(line, "Installed:"))
		case strings.HasPrefix(line, "Candidate:"):
			candidate = strings.TrimSpace(strings.TrimPrefix(line, "Candidate:"))
		}
	}
	return installed, candidate
}

// aptRpmInstall installs whichever of pkgspec is not already provided
// (or, for state=latest, not at-least-candidate-version), matching
// real apt_rpm's own install_packages: a single `apt-get -y install`
// call for every package that needs it, followed by a per-package
// re-check — a failure there (rc==0 but a package still isn't
// provided, matching real apt_rpm's own documented "apt-rpm always has
// 0 for exit code if --force is used" caveat) is reported via failMsg,
// not err, since it is an expected, well-formed failure (matching real
// apt_rpm's own fail_json call there rather than a crash).
func aptRpmInstall(ctx context.Context, conn remoteexec.Connection, pkgspec []string, allowUpgrade bool) (changed bool, msg, failMsg string, err error) {
	if len(pkgspec) == 0 {
		return false, "Empty package list", "", nil
	}
	var toInstall []string
	for _, pkg := range pkgspec {
		provided, err := aptRpmProvides(ctx, conn, pkg, allowUpgrade)
		if err != nil {
			return false, "", "", err
		}
		if !provided {
			toInstall = append(toInstall, pkg)
		}
	}
	if len(toInstall) == 0 {
		return false, "Nothing to install", "", nil
	}

	if _, err := run(ctx, conn, "env LANGUAGE=C LC_ALL=C apt-get -y install "+quoteAll(toInstall)); err != nil {
		return false, "", "", err
	}

	for _, pkg := range pkgspec {
		provided, err := aptRpmProvides(ctx, conn, pkg, false)
		if err != nil {
			return false, "", "", err
		}
		if !provided {
			return false, "", fmt.Sprintf("apt_rpm: 'apt-get -y install %s' failed", strings.Join(toInstall, " ")), nil
		}
	}
	return true, fmt.Sprintf("%v present(s)", toInstall), "", nil
}

// aptRpmRemove removes whichever of packages is currently installed
// (checked via plain `rpm -q`, matching real apt_rpm's own
// query_package — NOT `--provides`, unlike aptRpmProvides above; real
// apt_rpm's own remove_packages() genuinely uses a different, plainer
// query than install_packages() does), one `apt-get -y remove` call
// per package, matching real apt_rpm's own per-package loop.
func aptRpmRemove(ctx context.Context, conn remoteexec.Connection, packages []string) (changed bool, msg string, err error) {
	if len(packages) == 0 {
		return false, "Empty package list", nil
	}
	removed := 0
	for _, pkg := range packages {
		res, err := runStatus(ctx, conn, "rpm -q "+shellQuote(pkg)+" >/dev/null 2>&1")
		if err != nil {
			return false, "", err
		}
		if res.RC != 0 {
			continue
		}
		if _, err := run(ctx, conn, "env LANGUAGE=C LC_ALL=C apt-get -y remove "+shellQuote(pkg)); err != nil {
			return false, "", err
		}
		removed++
	}
	if removed > 0 {
		return true, fmt.Sprintf("removed %d package(s)", removed), nil
	}
	return false, "package(s) already absent", nil
}
