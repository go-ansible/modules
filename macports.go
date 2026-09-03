package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMacports implements (a subset of) Ansible's `macports` module:
// manages ports on macOS via MacPorts' `port` command.
//
// Args: name (string or []string, alias port) — a port name, or list of
// names; state (present|installed|absent|removed|active|inactive,
// default "present") — active/inactive select between two already-
// installed versions of a port (MacPorts allows several versions
// installed side by side, only one "active" at a time) and fail if the
// port isn't installed at all; selfupdate (bool, aliases update_cache/
// update_ports, default false) — runs `port -v selfupdate`, reported
// changed only when its output actually reports parsed ports or a new
// release (matching real macports.py's own output-sniffing, rather than
// this batch's usual "always changed" convention for whole-system
// operations, since real macports.py already does the more precise
// thing here and it costs nothing extra to match it); upgrade (bool,
// default false) — runs `port upgrade outdated`, reported changed
// unless its output is exactly "Nothing to upgrade." (again matching
// real macports.py's own check); variant (string, alias variants) — a
// `+foo+bar`-style variant spec appended to `port install`, only
// meaningful with state=present/installed.
//
// selfupdate and upgrade each run before any name is processed
// (matching real macports.py's own ordering), and either can be used
// standalone with no name given.
//
// Simplifications vs real macports: none beyond the above — this
// module's command shapes and idempotency checks (`port -q installed
// <name>`, checking the output starts with "<name> " for present, or
// contains "(active)" for active) match real macports.py directly.
func moduleMacports(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	state := argString(args, "state", "present")
	switch state {
	case "present", "installed", "absent", "removed", "active", "inactive":
	default:
		return Result{}, errArg("macports: state must be present, absent, active, or inactive, got %q", state)
	}

	changed := false
	var msgs []string

	if argBool(args, "selfupdate", false) {
		out, err := run(ctx, conn, "port -v selfupdate")
		if err != nil {
			return Result{}, err
		}
		if macportsSelfupdateChanged(out) {
			changed = true
			msgs = append(msgs, "Macports updated successfully")
		} else {
			msgs = append(msgs, "Macports already up-to-date")
		}
	}

	if argBool(args, "upgrade", false) {
		res, err := runStatus(ctx, conn, "port upgrade outdated")
		if err != nil {
			return Result{}, err
		}
		if strings.TrimSpace(res.Stdout) == "Nothing to upgrade." {
			msgs = append(msgs, "Ports already upgraded")
		} else if res.RC == 0 {
			changed = true
			msgs = append(msgs, "Outdated ports upgraded successfully")
		} else {
			return Fail("port upgrade outdated: " + strings.TrimSpace(res.Stderr)), nil
		}
	}

	if len(names) == 0 {
		if len(msgs) == 0 {
			return Result{}, errArg("macports: at least one of name, selfupdate, or upgrade is required")
		}
		return Result{Changed: changed, Msg: strings.Join(msgs, "; ")}, nil
	}

	var op Result
	var err error
	switch state {
	case "present", "installed":
		op, err = macportsInstall(ctx, conn, names, argString(args, "variant", ""))
	case "absent", "removed":
		op, err = macportsRemove(ctx, conn, names)
	case "active":
		op, err = macportsActivate(ctx, conn, names, true)
	case "inactive":
		op, err = macportsActivate(ctx, conn, names, false)
	}
	if err != nil {
		return Result{}, err
	}
	if op.Failed {
		return op, nil
	}
	msgs = append(msgs, op.Msg)
	return Result{Changed: changed || op.Changed, Msg: strings.Join(msgs, "; ")}, nil
}

func macportsSelfupdateChanged(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "Installing new Macports release") {
			return true
		}
		if idx := strings.Index(line, "Total number of ports parsed:"); idx >= 0 {
			rest := strings.TrimSpace(line[idx+len("Total number of ports parsed:"):])
			if rest != "0" && rest != "" {
				return true
			}
		}
	}
	return false
}

func macportsQuery(ctx context.Context, conn remoteexec.Connection, name string, active bool) (bool, error) {
	res, err := runStatus(ctx, conn, "port -q installed "+shellQuote(name))
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, nil
	}
	if active {
		return strings.Contains(res.Stdout, "(active)"), nil
	}
	return strings.HasPrefix(strings.TrimSpace(res.Stdout), name+" "), nil
}

func macportsInstall(ctx context.Context, conn remoteexec.Connection, names []string, variant string) (Result, error) {
	count := 0
	for _, name := range names {
		present, err := macportsQuery(ctx, conn, name, false)
		if err != nil {
			return Result{}, err
		}
		if present {
			continue
		}
		cmd := "port install " + shellQuote(name)
		if variant != "" {
			cmd += " " + shellQuote(variant)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		count++
	}
	if count > 0 {
		return Changed("Installed port(s)"), nil
	}
	return Ok("Port(s) already present"), nil
}

func macportsRemove(ctx context.Context, conn remoteexec.Connection, names []string) (Result, error) {
	count := 0
	for _, name := range names {
		present, err := macportsQuery(ctx, conn, name, false)
		if err != nil {
			return Result{}, err
		}
		if !present {
			continue
		}
		if _, err := run(ctx, conn, "port uninstall "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		count++
	}
	if count > 0 {
		return Changed("Removed port(s)"), nil
	}
	return Ok("Port(s) already absent"), nil
}

// macportsActivate implements state=active/inactive: unlike
// install/remove, real macports fails outright (module.fail_json, a
// well-formed "the request can't be satisfied" outcome, not a crash) if
// asked to (de)activate a port that isn't installed at all — there is
// no version to pick between.
func macportsActivate(ctx context.Context, conn remoteexec.Connection, names []string, activate bool) (Result, error) {
	verb, adj := "activate", "active"
	if !activate {
		verb, adj = "deactivate", "inactive"
	}
	count := 0
	for _, name := range names {
		present, err := macportsQuery(ctx, conn, name, false)
		if err != nil {
			return Result{}, err
		}
		if !present {
			return Fail("failed to " + verb + " " + name + ", port(s) not present"), nil
		}
		isActive, err := macportsQuery(ctx, conn, name, true)
		if err != nil {
			return Result{}, err
		}
		if isActive == activate {
			continue
		}
		if _, err := run(ctx, conn, "port "+verb+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		count++
	}
	if count > 0 {
		return Changed("Ports " + verb + "d"), nil
	}
	return Ok("Port(s) already " + adj), nil
}
