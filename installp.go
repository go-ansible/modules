package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleInstallp implements (a subset of) Ansible's `installp` module
// (deprecated upstream in favor of the `ibm.power_aix` collection, but
// still a real community.general module): installs/removes AIX
// filesets via `installp`.
//
// Args: name ([]string, required, alias pkg) — one or more package/
// fileset names, or "all" to install every package available in
// repository_path; repository_path (string, required for state=
// present) — a directory of AIX packages; accept_license (bool,
// default false) — `-Y`; state (present|absent, default "present").
//
// Presence is checked via `lslpp -lcq <name>*` (rc==0 => installed;
// rc==1 with ": not installed." in stderr => not installed, matching
// real installp's own _check_installed_pkg() exactly). For
// state=present, a name is additionally checked against `installp -l
// -MR -d repository_path`'s own listing (`installp -l` lists every
// package/fileset available in that repository) via a simple
// substring match — real installp instead applies name as a regular
// expression against each listing line and captures the matched
// name/version; this port's simplified substring match does not
// distinguish a package name from a fileset name the way real
// installp's regex/capture-group logic does, and does not report the
// "already installed: {name: version}" detail real installp's msg
// does — only whether a name was found in the repository at all.
// name=="all" always counts as found, without listing the repository
// (matching real installp's own `if package == "all": return True,
// "All packages on dir"`). A name found in the repository but not yet
// installed is installed via `installp -a [-Y] -X -d repository_path
// name`; a name not found in the repository is reported (not
// installed, not an error) in the result message, matching real
// installp exactly. For state=absent, an installed name is removed
// via `installp -u name`.
func moduleInstallp(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		names = argStringList(args, "pkg")
	}
	if len(names) == 0 {
		return Result{}, errArg("installp: missing required argument: name (or its alias pkg)")
	}
	state := argString(args, "state", "present")

	switch state {
	case "present":
		repositoryPath := argString(args, "repository_path", "")
		if repositoryPath == "" {
			return Result{}, errArg("installp: repository_path is required to install package")
		}
		acceptLicense := argBool(args, "accept_license", false)
		return installpInstall(ctx, conn, names, repositoryPath, acceptLicense)
	case "absent":
		return installpRemove(ctx, conn, names)
	default:
		return Result{}, errArg("installp: state must be present or absent, got %q", state)
	}
}

func installpInstalled(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "lslpp -lcq "+shellQuote(name+"*"))
	if err != nil {
		return false, fmt.Errorf("checking installp package %s: %w", name, err)
	}
	if res.RC == 0 {
		return true, nil
	}
	if res.RC == 1 && strings.Contains(res.Stderr, "not installed.") {
		return false, nil
	}
	return false, fmt.Errorf("checking installp package %s: lslpp exited %d: %s", name, res.RC, strings.TrimSpace(res.Stderr))
}

func installpAvailable(ctx context.Context, conn remoteexec.Connection, name, repositoryPath string) (bool, error) {
	if name == "all" {
		return true, nil
	}
	out, err := run(ctx, conn, "installp -l -MR -d "+shellQuote(repositoryPath))
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, name) {
			return true, nil
		}
	}
	return false, nil
}

func installpInstall(ctx context.Context, conn remoteexec.Connection, names []string, repositoryPath string, acceptLicense bool) (Result, error) {
	var installed, alreadyInstalled, notFound []string
	for _, name := range names {
		found, err := installpAvailable(ctx, conn, name, repositoryPath)
		if err != nil {
			return Result{}, err
		}
		if !found {
			notFound = append(notFound, name)
			continue
		}
		already, err := installpInstalled(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if already {
			alreadyInstalled = append(alreadyInstalled, name)
			continue
		}
		cmd := "installp -a"
		if acceptLicense {
			cmd += " -Y"
		}
		cmd += " -X -d " + shellQuote(repositoryPath) + " " + shellQuote(name)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		installed = append(installed, name)
	}

	msg := ""
	if len(installed) > 0 {
		msg += " Installed: " + strings.Join(installed, " ") + "."
	}
	if len(notFound) > 0 {
		msg += " Not found: " + strings.Join(notFound, " ") + "."
	}
	if len(alreadyInstalled) > 0 {
		msg += " Already installed: " + strings.Join(alreadyInstalled, " ") + "."
	}
	if len(installed) == 0 {
		return Ok(strings.TrimSpace("No packages installed." + msg)), nil
	}
	return Changed(strings.TrimSpace(msg)), nil
}

func installpRemove(ctx context.Context, conn remoteexec.Connection, names []string) (Result, error) {
	var removed, notFound []string
	for _, name := range names {
		present, err := installpInstalled(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if !present {
			notFound = append(notFound, name)
			continue
		}
		if _, err := run(ctx, conn, "installp -u "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		removed = append(removed, name)
	}
	if len(removed) == 0 {
		return Ok("No packages removed, all packages not found: " + strings.Join(notFound, " ")), nil
	}
	msg := "Packages removed: " + strings.Join(removed, " ") + "."
	if len(notFound) > 0 {
		msg += " Package(s) not found: " + strings.Join(notFound, " ")
	}
	return Changed(msg), nil
}
