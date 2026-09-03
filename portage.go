package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePortage implements (a subset of) Ansible's `portage` module:
// manages Gentoo packages via `emerge`.
//
// Args: package (string or []string, alias name) — a package atom or
// set, e.g. "sys-apps/foo" or "@world"; state (present|installed|
// emerged|latest|absent|removed|unmerged, default "present"); update
// (bool, default false) — `--update`; deep (bool, default false) —
// `--deep`; newuse (bool, default false) — `--newuse`; oneshot (bool,
// default false) — `--oneshot`; noreplace (bool, default true) —
// `--noreplace`, and also gates this port's own "skip if every atom is
// already installed" idempotency check (see below); nodeps (bool,
// default false) — `--nodeps`; quiet (bool, default false) —
// `--quiet`; verbose (bool, default false) — `--verbose`; getbinpkg
// (bool, default false) — `--getbinpkg`; usepkg (bool, default false)
// — `--usepkg`; usepkgonly (bool, default false) — `--usepkgonly`;
// keepgoing (bool, default false) — `--keep-going`; sync (""|"yes"|
// "web", default "") — "yes" runs `emerge --sync --quiet --ask=n`
// first, "web" runs `emerge-webrsync --quiet` first; when package is
// empty and depclean is false, a sync-only call stops there; depclean
// (bool, default false) — runs `emerge --depclean` instead of a normal
// merge/unmerge; with no package given, cleans the whole world's
// unneeded dependencies; with package given, only valid alongside an
// absent-family state (errArg otherwise, matching real portage's own
// fail_json). At least one of package/sync/depclean is required.
//
// Simplifications vs real portage: no backtrack/jobs/loadavg/
// withbdeps/changed_deps/changed_use/select/quietbuild/quietfail/
// getbinpkgonly/onlydeps support, and no mutually_exclusive validation
// between flags (e.g. nodeps+onlydeps) since onlydeps itself is not
// implemented; check_mode is not modeled (see zfs_delegate_admin.go's
// own doc comment). Idempotency: a package atom's installed state is
// checked via `qlist -Ie <atom>` (portage-utils' qlist, commonly
// installed as app-portage/portage-utils on Gentoo) — real portage
// instead queries Gentoo's own `portage.dbapi.vartree` Python API
// in-process, which this port has no access to, never running code on
// the target beyond shell commands; a set atom (starting with "@") is
// never treated as already-installed by this probe (real portage
// checks `/var/lib/portage/world_sets` for those instead — a narrower,
// set-specific file this port does not parse), so present/absent
// against a set atom always runs emerge. For present/installed/
// emerged/latest, when noreplace is true and update/newuse/state=latest
// are all unset, and every named atom already queries as installed,
// this port reports Ok(unchanged) without running emerge at all
// (matching real portage's own pre-check); otherwise `emerge` runs and
// this port scans its stdout for a line matching `^>+ Emerging
// (?:binary )?\(1 of` to decide Changed, exactly matching real
// portage's own output-scanning logic (an emerge run that installs
// nothing prints no such line). For absent/removed/unmerged, if NONE
// of the named atoms currently query as installed, this port reports
// Ok(unchanged) without running `emerge --unmerge`; otherwise it runs
// unconditionally reports Changed=true (real portage does the same:
// unmerge_packages() never re-scans output the way emerge_packages()
// does). depclean scans stdout for a "Number removed: N" line and
// reports Changed=(N>0), exactly matching real portage.
func modulePortage(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	packages := argStringList(args, "package")
	if len(packages) == 0 {
		packages = argStringList(args, "name")
	}
	sync := argString(args, "sync", "")
	if sync != "" && sync != "yes" && sync != "web" && sync != "no" {
		return Result{}, errArg("portage: sync must be yes, web, or no, got %q", sync)
	}
	depclean := argBool(args, "depclean", false)
	if len(packages) == 0 && sync == "" && !depclean {
		return Result{}, errArg("portage: one of package, sync, or depclean is required")
	}
	state := argString(args, "state", "present")

	if sync == "yes" || sync == "web" {
		var err error
		if sync == "web" {
			_, err = run(ctx, conn, "emerge-webrsync --quiet")
		} else {
			_, err = run(ctx, conn, "emerge --sync --quiet --ask=n")
		}
		if err != nil {
			return Result{}, err
		}
		if len(packages) == 0 && !depclean {
			return Ok("Sync successfully finished."), nil
		}
	}

	if depclean {
		if len(packages) > 0 && !portageAbsentState(state) {
			return Result{}, errArg("portage: depclean can only be used with package when state is absent, removed, or unmerged")
		}
		return portageDepclean(ctx, conn, packages, args)
	}
	if portageAbsentState(state) {
		return portageUnmerge(ctx, conn, packages, args)
	}
	return portageEmerge(ctx, conn, packages, state, args)
}

func portageAbsentState(state string) bool {
	switch state {
	case "absent", "removed", "unmerged":
		return true
	}
	return false
}

var portageEmergingRe = regexp.MustCompile(`^>+\s*Emerging (?:binary )?\(1 of`)

func portageEmerge(ctx context.Context, conn remoteexec.Connection, packages []string, state string, args map[string]any) (Result, error) {
	noreplace := argBool(args, "noreplace", true)
	update := argBool(args, "update", false)
	newuse := argBool(args, "newuse", false)

	if noreplace && !newuse && !update && state != "latest" {
		allInstalled := true
		for _, p := range packages {
			installed, err := portageQuery(ctx, conn, p)
			if err != nil {
				return Result{}, err
			}
			if !installed {
				allInstalled = false
				break
			}
		}
		if allInstalled {
			return Ok("Packages already present."), nil
		}
	}

	cmd := "emerge"
	flags := map[string]string{
		"update": "--update", "deep": "--deep", "newuse": "--newuse",
		"oneshot": "--oneshot", "noreplace": "--noreplace", "nodeps": "--nodeps",
		"quiet": "--quiet", "verbose": "--verbose", "getbinpkg": "--getbinpkg",
		"usepkg": "--usepkg", "usepkgonly": "--usepkgonly", "keepgoing": "--keep-going",
	}
	for _, key := range []string{"update", "deep", "newuse", "oneshot", "noreplace", "nodeps",
		"quiet", "verbose", "getbinpkg", "usepkg", "usepkgonly", "keepgoing"} {
		def := key == "noreplace"
		if argBool(args, key, def) {
			cmd += " " + flags[key]
		}
	}
	if state == "latest" {
		cmd += " --update"
	}
	cmd += " --ask=n " + quoteAll(packages)

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("portage: emerge failed: %s", strings.TrimSpace(res.Stderr))), nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if portageEmergingRe.MatchString(line) {
			return Changed("Packages installed."), nil
		}
	}
	return Ok("No packages installed."), nil
}

func portageUnmerge(ctx context.Context, conn remoteexec.Connection, packages []string, args map[string]any) (Result, error) {
	anyInstalled := false
	for _, p := range packages {
		installed, err := portageQuery(ctx, conn, p)
		if err != nil {
			return Result{}, err
		}
		if installed {
			anyInstalled = true
			break
		}
	}
	if !anyInstalled {
		return Ok("Packages already absent."), nil
	}

	cmd := "emerge --unmerge"
	if argBool(args, "quiet", false) {
		cmd += " --quiet"
	}
	if argBool(args, "verbose", false) {
		cmd += " --verbose"
	}
	cmd += " --ask=n " + quoteAll(packages)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed("Packages removed."), nil
}

var portageNumberRemovedRe = regexp.MustCompile(`^Number removed:\s*(\d+)`)

func portageDepclean(ctx context.Context, conn remoteexec.Connection, packages []string, args map[string]any) (Result, error) {
	if len(packages) > 0 {
		anyInstalled := false
		for _, p := range packages {
			installed, err := portageQuery(ctx, conn, p)
			if err != nil {
				return Result{}, err
			}
			if installed {
				anyInstalled = true
				break
			}
		}
		if !anyInstalled {
			return Ok("Packages already absent."), nil
		}
	}

	cmd := "emerge --depclean"
	if argBool(args, "quiet", false) {
		cmd += " --quiet"
	}
	if argBool(args, "verbose", false) {
		cmd += " --verbose"
	}
	cmd += " --ask=n"
	if len(packages) > 0 {
		cmd += " " + quoteAll(packages)
	}
	out, err := run(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	removed := 0
	for _, line := range strings.Split(out, "\n") {
		if m := portageNumberRemovedRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			fmt.Sscanf(m[1], "%d", &removed)
		}
	}
	if removed > 0 {
		return Changed("Depclean completed."), nil
	}
	return Ok("Depclean completed."), nil
}

// portageQuery reports whether atom is currently installed, via
// `qlist -Ie <atom>` — see the doc comment above for why this port
// cannot use portage's own vartree API, and why a "@"-prefixed set
// atom is always reported not-installed.
func portageQuery(ctx context.Context, conn remoteexec.Connection, atom string) (bool, error) {
	if strings.HasPrefix(atom, "@") {
		return false, nil
	}
	res, err := runStatus(ctx, conn, "qlist -Ie "+shellQuote(atom)+" >/dev/null 2>&1")
	if err != nil {
		return false, fmt.Errorf("checking portage package %s: %w", atom, err)
	}
	return res.RC == 0, nil
}
