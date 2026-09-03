package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePortinstall implements (a subset of) Ansible's `portinstall`
// module: installs/removes FreeBSD packages via `portinstall` (from
// the ports-collection `portupgrade` tools) for install, and `pkg
// delete` for removal.
//
// Args: name (string, required, alias pkg) — one package name, or
// several separated by commas (matching real portinstall's own
// `name.split(",")`, since its argspec never declares name as a
// list); state (present|absent, default "present"); use_packages
// (bool, default true) — `--use-packages`, prefer binary packages over
// building from ports.
//
// Simplifications vs real portinstall: real portinstall auto-detects
// between the legacy `pkg_info`/`pkg_delete`/`pkg_glob` tools and
// modern `pkg`(ng), retries a package name with digits stripped (e.g.
// "mysql55-client" installing as "mysql-client"), and counts
// `ports_glob` matches to catch an ambiguous/missing port name before
// attempting the install; this port always uses the pkgng-style query
// (`pkg info -e <name>`, matching pkgng.go's own convention) and
// direct `pkg delete -y <name>` for removal, with none of the digit-
// stripping retry or ports_glob match-counting — a package name must
// match pkgng's own installed-package name exactly.
//
// Each package is queried, then (for present) `portinstall --batch
// [--use-packages] <name>` is run for any not-yet-installed name; (for
// absent) `pkg delete -y <name>` is run for any still-installed name.
// Real portinstall additionally re-queries after each install/remove
// and fails explicitly if the package still is not in the expected
// state; this port instead trusts `portinstall`/`pkg delete`'s own
// exit code (via run(), which already turns a non-zero exit into a Go
// error) — matching this batch's house convention elsewhere (e.g.
// pkgng.go, pkgutil.go) of not re-verifying after a batch package
// manager operation reports success.
func modulePortinstall(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	nameArg := argString(args, "name", argString(args, "pkg", ""))
	if nameArg == "" {
		return Result{}, errArg("portinstall: missing required argument: name (or its alias pkg)")
	}
	var packages []string
	for _, p := range strings.Split(nameArg, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			packages = append(packages, p)
		}
	}
	state := argString(args, "state", "present")
	usePackages := argBool(args, "use_packages", true)

	switch state {
	case "present":
		return portinstallInstall(ctx, conn, packages, usePackages)
	case "absent":
		return portinstallRemove(ctx, conn, packages)
	default:
		return Result{}, errArg("portinstall: state must be present or absent, got %q", state)
	}
}

func portinstallQuery(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "pkg info -e "+shellQuote(name)+" >/dev/null 2>&1")
	if err != nil {
		return false, fmt.Errorf("checking portinstall package %s: %w", name, err)
	}
	return res.RC == 0, nil
}

func portinstallInstall(ctx context.Context, conn remoteexec.Connection, packages []string, usePackages bool) (Result, error) {
	flags := ""
	if usePackages {
		flags = " --use-packages"
	}
	var installed []string
	for _, pkg := range packages {
		already, err := portinstallQuery(ctx, conn, pkg)
		if err != nil {
			return Result{}, err
		}
		if already {
			continue
		}
		if _, err := run(ctx, conn, "portinstall --batch"+flags+" "+shellQuote(pkg)); err != nil {
			return Result{}, err
		}
		installed = append(installed, pkg)
	}
	if len(installed) == 0 {
		return Ok("package(s) already present"), nil
	}
	return Changed(fmt.Sprintf("present %d package(s)", len(installed))), nil
}

func portinstallRemove(ctx context.Context, conn remoteexec.Connection, packages []string) (Result, error) {
	var removed []string
	for _, pkg := range packages {
		present, err := portinstallQuery(ctx, conn, pkg)
		if err != nil {
			return Result{}, err
		}
		if !present {
			continue
		}
		if _, err := run(ctx, conn, "pkg delete -y "+shellQuote(pkg)); err != nil {
			return Result{}, err
		}
		removed = append(removed, pkg)
	}
	if len(removed) == 0 {
		return Ok("package(s) already absent"), nil
	}
	return Changed(fmt.Sprintf("removed %d package(s)", len(removed))), nil
}
