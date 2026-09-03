package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCpanm implements (a subset of) Ansible's `cpanm` module:
// installs Perl distributions via `cpanm` (App::cpanminus).
//
// Args: name (string, aliases "pkg" via the args map directly — see
// below) — a module name, distribution file, HTTP URL, or git URL, per
// cpanm's own `mode=new` syntax (the only mode this port, and current
// community.general 13.x, supports — `compatibility` mode was removed
// upstream in 13.0.0); mode (string, must be "new" if given at all);
// version (string, optional) — appended to name as `name~version`,
// cpanm's own version-constraint syntax; from_path (string, optional) —
// installs from a local directory or tarball instead of `name`;
// locallib (string, optional) — `--local-lib`; mirror (string,
// optional) — `--mirror`; mirror_only (bool, default false) —
// `--mirror-only`; notest (bool, default false) — `--notest`;
// installdeps (bool, default false) — `--installdeps`;
// install_recommendations, install_suggestions (bool, tri-state: unset
// leaves cpanm's own default/PERL_CPANM_OPT alone) —
// `--with(out)-recommends`/`--with(out)-suggests`; name_check (string,
// optional) — the Perl module name to probe for "already installed"
// when it differs from `name` (e.g. `name` is a tarball or URL);
// executable (string, default "cpanm").
//
// Simplifications vs real cpanm: cpanm has no separate query
// subcommand, so this port takes two shortcuts — both worth a second
// look against a real cpanm binary, since they're inferred from cpanm's
// general behavior rather than read off the fetched ansible-doc text:
// (1) when installing a bare module name with no `version` pin, it
// first probes with `perl -M<name_check or name> -e1` and skips the
// install entirely if that succeeds (cheap, but only checks "is
// loadable at all", not "at the version cpanm would consider
// satisfying"); (2) otherwise it always runs cpanm and classifies the
// result by searching its stdout for the case-insensitive substring "is
// up to date" (the message cpanm is believed to print when a target is
// already current) to decide changed vs unchanged — if that string
// doesn't match cpanm's actual wording, every run reports changed
// instead of being silently wrong the other way. `from_path` skips both
// checks (always changed), matching bundler.go's own no-cheap-probe
// tradeoff.
func moduleCpanm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	nameArgs := args
	if _, ok := args["name"]; !ok {
		if v, ok := args["pkg"]; ok {
			nameArgs = map[string]any{"name": v}
		}
	}
	name := argString(nameArgs, "name", "")
	fromPath := argString(args, "from_path", "")
	if name == "" && fromPath == "" {
		return Result{}, errArg("cpanm: one of name or from_path is required")
	}
	mode := argString(args, "mode", "new")
	if mode != "new" {
		return Result{}, errArg("cpanm: mode must be \"new\" (compatibility mode was removed upstream)")
	}
	version := argString(args, "version", "")
	locallib := argString(args, "locallib", "")
	mirror := argString(args, "mirror", "")
	mirrorOnly := argBool(args, "mirror_only", false)
	notest := argBool(args, "notest", false)
	installdeps := argBool(args, "installdeps", false)
	nameCheck := argString(args, "name_check", "")
	exe := argString(args, "executable", "cpanm")

	recommends, recommendsSet := cpanmTriBool(args, "install_recommendations")
	suggests, suggestsSet := cpanmTriBool(args, "install_suggestions")

	checkName := nameCheck
	if checkName == "" {
		checkName = name
	}
	if fromPath == "" && version == "" && checkName != "" {
		res, err := conn.Exec(ctx, "perl -M"+checkName+" -e1 >/dev/null 2>&1", nil)
		if err != nil {
			return Result{}, err
		}
		if res.RC == 0 {
			return Ok(checkName + " already installed"), nil
		}
	}

	cmd := exe
	if notest {
		cmd += " --notest"
	}
	if installdeps {
		cmd += " --installdeps"
	}
	if locallib != "" {
		cmd += " --local-lib " + shellQuote(locallib)
	}
	if mirror != "" {
		cmd += " --mirror " + shellQuote(mirror)
	}
	if mirrorOnly {
		cmd += " --mirror-only"
	}
	if recommendsSet {
		if recommends {
			cmd += " --with-recommends"
		} else {
			cmd += " --without-recommends"
		}
	}
	if suggestsSet {
		if suggests {
			cmd += " --with-suggests"
		} else {
			cmd += " --without-suggests"
		}
	}

	var target string
	switch {
	case fromPath != "":
		target = fromPath
	case version != "":
		target = name + "~" + version
	default:
		target = name
	}
	cmd += " " + shellQuote(target)

	out, err := run(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if strings.Contains(strings.ToLower(out), "is up to date") {
		return Ok(target + " is up to date"), nil
	}
	return Changed(target + " installed"), nil
}

// cpanmTriBool reads a tri-state boolean argument: set=false means the
// key was absent entirely (real cpanm's "leave PERL_CPANM_OPT alone"),
// distinct from an explicit false value.
func cpanmTriBool(args map[string]any, key string) (val bool, set bool) {
	v, ok := args[key]
	if !ok {
		return false, false
	}
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		if parsed, err := strconv.ParseBool(b); err == nil {
			return parsed, true
		}
	}
	return false, false
}
