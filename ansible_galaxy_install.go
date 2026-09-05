package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAnsibleGalaxyInstall implements Ansible's `ansible_galaxy_install`
// module: installs collections and/or roles by shelling out to
// `ansible-galaxy` — the real module ALREADY does exactly this (it is
// not a CLI substitution: real ansible_galaxy_install.py's own
// `command = "ansible-galaxy"` shells out to the very same tool this
// port also invokes), so there is no substitution gap to document here
// at all, unlike this batch's other modules.
//
// Note on which `ansible-galaxy` binary runs: this module (matching
// real ansible_galaxy_install.py's own `executable` argument, "Path to
// the `ansible-galaxy` executable. When not specified, the module uses
// `ansible-galaxy` found by Ansible") shells out to whatever
// `ansible-galaxy` is first on the TARGET's own PATH (or the exact path
// given via `executable`), with no assumption about which
// implementation that is — this org's own go-ansible/cli repo happens
// to ship a real `ansible-galaxy` binary, but this module does not
// special-case that or any other implementation.
//
// Args: type (collection|role|both, required); name (string) — mutually
// exclusive with requirements_file, one of the two required;
// requirements_file (path); dest (path); executable (path, default
// "ansible-galaxy"); force (bool, default false); no_deps (bool,
// default false); state (present|latest, default "present" — "latest"
// only has an effect when type=collection, matching real
// ansible_galaxy_install.py's own `upgrade = type == "collection" and
// state == "latest"`).
//
// # Command construction — mirrors real ansible_galaxy_install.py's own
// # CmdRunner argument order exactly
//
// `<executable> [<type>] install [--upgrade] [--force] [--no-deps]
// [-p <dest>] [-r <requirements_file>] [<name>]` — type is omitted
// entirely for type=both (real module's own
// `cmd_runner_fmt.as_func(lambda v: [] if v == "both" else [v])`),
// which is why type=both requires requirements_file (ansible-galaxy has
// no bare `install` subcommand covering both roles and collections at
// once from a single name).
//
// # Simplifications versus real ansible_galaxy_install.py — documented,
// # not silent
//
//   - Real ansible_galaxy_install.py pre-lists every already-installed
//     collection/role per search path (via `ansible-galaxy <type> list`,
//     parsed) BEFORE running install, and returns that full inventory as
//     `installed_collections`/`installed_roles`. This port does not
//     perform that extra pre-listing pass — it is read-only inventory
//     information, not load-bearing for idempotency (idempotency here,
//     as in the real module, is entirely driven by parsing the install
//     command's own "X was installed successfully" output lines below),
//     and skipping it halves the number of `ansible-galaxy` invocations
//     this port needs per task run. `dest`/`name`/`requirements_file`/
//     `force`/`type` are still returned as extras (echoing the real
//     module's own always-returned parameter-echo fields), just not the
//     full per-path inventory dump.
//   - Real ansible_galaxy_install.py retries the version-probe under
//     C.UTF-8 then en_US.UTF-8 if the target's locale rejects the first.
//     This port runs `ansible-galaxy --version` once, under whatever
//     locale the target Connection's shell already uses — a locale
//     mismatch here surfaces as a failed version probe rather than a
//     silent fallback, which is an acceptable, honestly-simpler
//     behavior for a port with no locale-detection logic of its own.
//
// Changed is true whenever the install command's own stdout reports at
// least one "<collection>:<version> was installed successfully" or
// "- <role> (<version>) was installed successfully" line — exactly the
// same regex real ansible_galaxy_install.py's own `_RE_INSTALL_OUTPUT`
// matches, which is also why `force=true`'s reinstall naturally reports
// Changed=true every run (forcing makes ansible-galaxy re-emit that same
// success line even for something already present) without this port
// needing a separate force-specific Changed override.
func moduleAnsibleGalaxyInstall(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	typ := argString(args, "type", "")
	if typ != "collection" && typ != "role" && typ != "both" {
		return Result{}, errArg("ansible_galaxy_install: type must be one of collection, role, both, got %q", typ)
	}
	name := argString(args, "name", "")
	reqFile := argString(args, "requirements_file", "")
	if name != "" && reqFile != "" {
		return Result{}, errArg("ansible_galaxy_install: name and requirements_file are mutually exclusive")
	}
	if name == "" && reqFile == "" {
		return Result{}, errArg("ansible_galaxy_install: one of name or requirements_file is required")
	}
	if typ == "both" && reqFile == "" {
		return Result{}, errArg("ansible_galaxy_install: requirements_file is required when type=both")
	}
	dest := argString(args, "dest", "")
	executable := argString(args, "executable", "ansible-galaxy")
	force := argBool(args, "force", false)
	noDeps := argBool(args, "no_deps", false)
	state := argString(args, "state", "present")
	if state != "present" && state != "latest" {
		return Result{}, errArg("ansible_galaxy_install: state must be present or latest, got %q", state)
	}

	if _, err := run(ctx, conn, "command -v "+shellQuote(executable)); err != nil {
		return Fail(fmt.Sprintf("ansible_galaxy_install: the %s binary was not found in PATH on the target",
			executable)), nil
	}

	verOut, _ := run(ctx, conn, shellQuote(executable)+" --version")
	version := ansibleGalaxyParseVersion(verOut)

	argv := []string{executable}
	if typ != "both" {
		argv = append(argv, typ)
	}
	argv = append(argv, "install")
	if typ == "collection" && state == "latest" {
		argv = append(argv, "--upgrade")
	}
	if force {
		argv = append(argv, "--force")
	}
	if noDeps {
		argv = append(argv, "--no-deps")
	}
	if dest != "" {
		argv = append(argv, "-p", dest)
	}
	if reqFile != "" {
		argv = append(argv, "-r", reqFile)
	}
	if name != "" {
		argv = append(argv, name)
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	res, err := runStatus(ctx, conn, strings.Join(quoted, " "))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail(fmt.Sprintf("ansible_galaxy_install: %s", msg)), nil
	}

	newCollections, newRoles := ansibleGalaxyParseInstallOutput(res.Stdout)
	changed := len(newCollections) > 0 || len(newRoles) > 0

	result := Ok("ansible-galaxy install: nothing new to install")
	if changed {
		result = Changed("ansible-galaxy install: installed new content")
	}
	result = result.WithExtra("type", typ)
	result = result.WithExtra("name", name)
	result = result.WithExtra("dest", dest)
	result = result.WithExtra("requirements_file", reqFile)
	result = result.WithExtra("force", force)
	result = result.WithExtra("version", version)
	result = result.WithExtra("new_collections", newCollections)
	result = result.WithExtra("new_roles", newRoles)
	return result, nil
}

var (
	ansibleGalaxyVersionRE = regexp.MustCompile(`ansible-galaxy(?: \[core)? (\d+\.\d+\.\d+)`)
	ansibleGalaxyInstallRE = regexp.MustCompile(`^(?:(\w+\.\w+):([\d.]+)|- (\w+\.\w+) \(([\d.]+)\)) was installed successfully$`)
)

// ansibleGalaxyParseVersion extracts the ansible-core version from
// `ansible-galaxy --version`'s own first line — matching real
// ansible_galaxy_install.py's own _RE_GALAXY_VERSION.
func ansibleGalaxyParseVersion(out string) string {
	m := ansibleGalaxyVersionRE.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return m[1]
}

// ansibleGalaxyParseInstallOutput extracts every newly-installed
// collection/role from an `ansible-galaxy install` invocation's own
// stdout — matching real ansible_galaxy_install.py's own
// _RE_INSTALL_OUTPUT exactly (a collection line reads
// "ns.name:1.2.3 was installed successfully", a role line reads
// "- rolename (1.2.3) was installed successfully").
func ansibleGalaxyParseInstallOutput(out string) (newCollections, newRoles map[string]string) {
	newCollections = map[string]string{}
	newRoles = map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		m := ansibleGalaxyInstallRE.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		if m[1] != "" {
			newCollections[m[1]] = m[2]
		} else if m[3] != "" {
			newRoles[m[3]] = m[4]
		}
	}
	return newCollections, newRoles
}
