package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePkgutil implements (a subset of) Ansible's `pkgutil` module:
// installs, updates, and removes OpenCSW packages on Solaris via
// `pkgutil`, which (unlike svr4pkg.go) resolves and downloads
// dependencies from a catalog.
//
// Args: name (string or []string, required, alias pkg) — a package
// name, or list of names; using state=latest, "*" updates every
// pkgutil-managed package; state (present|installed|latest|absent|
// removed, required); site (string, optional) — `-t site`, an
// alternate repository path/URL; update_catalog (bool, default false)
// — `-U`, force-refresh the catalog even when not stale; force (bool,
// default false) — `-f`, allow downgrading to match the catalog
// (present/latest operations only).
//
// Idempotency for present is checked via `pkginfo -q <name>` per
// package (exit 0 iff installed) — only the still-missing names are
// passed to `pkgutil -iy`. Idempotency for absent additionally
// requires the name to start with "CSW" (matching real pkgutil's own
// packages_installed() filter — a bare `pkginfo -q` match on a
// non-CSW-prefixed name is not treated as "this pkgutil-managed
// package is present"). state=latest queries the catalog via `pkgutil
// -c` (optionally with `-U`/`-t site`) and parses its own output table
// (skipping the header/footer lines, and any row containing "catalog"
// or "SAME") to find outdated names, exactly matching real pkgutil's
// own packages_not_latest(); a name that is either missing or outdated
// is passed to `pkgutil -uy`. name=["*"] with state=latest instead
// runs the catalog check with no explicit names and, if anything came
// back outdated, runs `pkgutil -uy` with NO package names at all
// (pkgutil's own convention for "update everything installed"),
// matching real pkgutil exactly; name=["*"] is rejected for
// present/installed (errArg), matching real pkgutil's own fail_json.
func modulePkgutil(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		names = argStringList(args, "pkg")
	}
	if len(names) == 0 {
		return Result{}, errArg("pkgutil: missing required argument: name (or its alias pkg)")
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	site := argString(args, "site", "")
	updateCatalog := argBool(args, "update_catalog", false)
	force := argBool(args, "force", false)
	flags := ""
	if updateCatalog {
		flags += " -U"
	}
	if site != "" {
		flags += " -t " + shellQuote(site)
	}
	installFlags := flags
	if force {
		installFlags += " -f"
	}

	switch state {
	case "present", "installed":
		if len(names) == 1 && names[0] == "*" {
			return Result{}, errArg("pkgutil: cannot use state=present with name '*'")
		}
		missing, err := pkgutilNotInstalled(ctx, conn, names)
		if err != nil {
			return Result{}, err
		}
		if len(missing) == 0 {
			return Ok("already installed"), nil
		}
		if _, err := run(ctx, conn, "pkgutil -iy"+installFlags+" "+quoteAll(missing)); err != nil {
			return Result{}, err
		}
		return Changed(strings.Join(missing, ", ")), nil

	case "absent", "removed":
		present, err := pkgutilInstalled(ctx, conn, names)
		if err != nil {
			return Result{}, err
		}
		if len(present) == 0 {
			return Ok("already absent"), nil
		}
		if _, err := run(ctx, conn, "pkgutil -ry "+quoteAll(present)); err != nil {
			return Result{}, err
		}
		return Changed(strings.Join(present, ", ")), nil

	case "latest":
		if len(names) == 1 && names[0] == "*" {
			outdated, err := pkgutilNotLatest(ctx, conn, names, flags)
			if err != nil {
				return Result{}, err
			}
			if len(outdated) == 0 {
				return Ok("already latest"), nil
			}
			if _, err := run(ctx, conn, "pkgutil -uy"+installFlags); err != nil {
				return Result{}, err
			}
			return Changed("system upgraded"), nil
		}
		missing, err := pkgutilNotInstalled(ctx, conn, names)
		if err != nil {
			return Result{}, err
		}
		outdated, err := pkgutilNotLatest(ctx, conn, names, flags)
		if err != nil {
			return Result{}, err
		}
		pkgs := append(missing, outdated...)
		if len(pkgs) == 0 {
			return Ok("already latest"), nil
		}
		if _, err := run(ctx, conn, "pkgutil -uy"+installFlags+" "+quoteAll(pkgs)); err != nil {
			return Result{}, err
		}
		return Changed(strings.Join(pkgs, ", ")), nil

	default:
		return Result{}, errArg("pkgutil: state must be present, installed, latest, absent, or removed, got %q", state)
	}
}

func pkgutilNotInstalled(ctx context.Context, conn remoteexec.Connection, names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		res, err := runStatus(ctx, conn, "pkginfo -q "+shellQuote(n))
		if err != nil {
			return nil, err
		}
		if res.RC != 0 {
			out = append(out, n)
		}
	}
	return out, nil
}

func pkgutilInstalled(ctx context.Context, conn remoteexec.Connection, names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		if !strings.HasPrefix(n, "CSW") {
			continue
		}
		res, err := runStatus(ctx, conn, "pkginfo -q "+shellQuote(n))
		if err != nil {
			return nil, err
		}
		if res.RC == 0 {
			out = append(out, n)
		}
	}
	return out, nil
}

// pkgutilNotLatest queries the catalog via `pkgutil -c` (flags carries
// any -U/-t site) for names (omitted entirely when names is ["*"]) and
// parses its table for outdated package names, matching real
// pkgutil's own packages_not_latest() exactly: drop the first and last
// output lines, then take the first whitespace-separated field of any
// remaining line that mentions neither "catalog" nor "SAME",
// deduplicated.
func pkgutilNotLatest(ctx context.Context, conn remoteexec.Connection, names []string, flags string) ([]string, error) {
	cmd := "pkgutil" + flags + " -c"
	if !(len(names) == 1 && names[0] == "*") {
		cmd += " " + quoteAll(names)
	}
	out, err := run(ctx, conn, cmd)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		return nil, nil
	}
	lines = lines[1 : len(lines)-1]
	seen := map[string]bool{}
	var result []string
	for _, line := range lines {
		if strings.Contains(line, "catalog") || strings.Contains(line, "SAME") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !seen[fields[0]] {
			seen[fields[0]] = true
			result = append(result, fields[0])
		}
	}
	return result, nil
}
