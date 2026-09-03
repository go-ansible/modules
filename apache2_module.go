package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleApache2Module implements (a subset of) Ansible's
// `apache2_module` module: enables or disables an Apache2 module via
// `a2enmod`/`a2dismod` — read from real apache2_module.py's own
// _module_is_enabled/create_apache_identifier/_set_state (this batch's
// hard rule: the exact identifier-guessing heuristics and the
// ignore_configcheck/warn_mpm_absent error-suppression branches are
// only visible in the implementation, not EXAMPLES/OPTIONS).
//
// Args: name (string, required); identifier (string, optional) —
// defaults to the value createApache2Identifier(name) computes (see
// its own doc comment); force (bool, default false) — passes `-f` to
// a2dismod (Debian's own override for its "this module protects a
// safe default configuration" warnings; has no effect on a2enmod);
// state (present|absent, default "present"); ignore_configcheck (bool,
// default false); warn_mpm_absent (bool, default true) — accepted for
// compatibility but has no observable effect here, since this port has
// no warnings channel separate from Result.Msg (see below).
//
// Requires apache2ctl or apachectl on the target (checked in that
// order, matching real _get_ctl_binary) to query the enabled-module
// list (`<ctl> -M`) and — only for name="cgi" and state=present — to
// probe for a threaded MPM (`<ctl> -V`, checked against the same
// `threaded: *yes` pattern real _run_threaded uses) and fail before
// touching anything, matching real main()'s own up-front check (cgi
// under a threaded MPM is unsupported by mod_cgi, not by this module).
//
// _module_is_enabled's own ignore_configcheck branches (log a warning
// and treat the module as "not enabled" rather than failing, when
// `<ctl> -M` itself errors) are reproduced functionally — this port
// silently swallows the message the same way, since Result carries no
// separate warnings list — but the warning text itself is lost; a
// documented simplification, not a behavioral gap in the actual
// present/absent decision it feeds.
//
// state is applied only if the module's current enabled/disabled
// status (per `<ctl> -M`, searching for " "+identifier as a substring
// — matching real _module_is_enabled exactly, spaces included) differs
// from what's wanted; `a2enmod`/`a2dismod name` (plus `-f` for
// a2dismod when force) is run, then re-verified against `<ctl> -M`
// once more — a failed re-verification is reported as Fail with a
// hint to check `identifier`, matching real _set_state's own message
// text.
func moduleApache2Module(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	identifier := argString(args, "identifier", "")
	if identifier == "" {
		identifier = createApache2Identifier(name)
	}
	force := argBool(args, "force", false)
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("apache2_module: state must be present or absent, got %q", state)
	}
	ignoreConfigcheck := argBool(args, "ignore_configcheck", false)

	ctl, err := apache2CtlBinary(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if ctl == "" {
		return Fail("apache2_module: neither apache2ctl nor apachectl found. At least one apache control binary is necessary."), nil
	}

	if name == "cgi" && state == "present" {
		threaded, err := apache2Threaded(ctx, conn, ctl)
		if err != nil {
			return Result{}, err
		}
		if threaded {
			return Fail("Your MPM seems to be threaded, therefore enabling cgi module is not allowed."), nil
		}
	}

	wantEnabled := state == "present"
	stateString := "disabled"
	a2modBinary := "a2dismod"
	if wantEnabled {
		stateString = "enabled"
		a2modBinary = "a2enmod"
	}
	successMsg := fmt.Sprintf("Module %s %s", name, stateString)

	enabled, failMsg, err := apache2ModuleEnabled(ctx, conn, ctl, identifier, name, ignoreConfigcheck)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	if enabled == wantEnabled {
		return Ok(successMsg), nil
	}

	if _, err := run(ctx, conn, "command -v "+a2modBinary); err != nil {
		return Fail(fmt.Sprintf("%s not found. Perhaps this system does not use %s to manage apache", a2modBinary, a2modBinary)), nil
	}

	cmd := a2modBinary
	if !wantEnabled && force {
		cmd += " -f"
	}
	cmd += " " + shellQuote(name)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	nowEnabled, failMsg, err := apache2ModuleEnabled(ctx, conn, ctl, identifier, name, ignoreConfigcheck)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}
	if nowEnabled == wantEnabled {
		return Changed(successMsg), nil
	}
	return Fail(fmt.Sprintf(
		"Failed to set module %s to %s:\n%s\nMaybe the module identifier (%s) was guessed incorrectly.Consider setting the \"identifier\" option.",
		name, stateString, res.Stdout, identifier,
	)), nil
}

// apache2CtlBinary returns "apache2ctl" or "apachectl", whichever is
// first found on PATH (matching real _get_ctl_binary's own check
// order), or "" if neither is present.
func apache2CtlBinary(ctx context.Context, conn remoteexec.Connection) (string, error) {
	for _, candidate := range []string{"apache2ctl", "apachectl"} {
		if _, err := run(ctx, conn, "command -v "+candidate); err == nil {
			return candidate, nil
		}
	}
	return "", nil
}

var apache2ThreadedPattern = regexp.MustCompile(`threaded: *yes`)

func apache2Threaded(ctx context.Context, conn remoteexec.Connection, ctl string) (bool, error) {
	// real _run_threaded ignores module.run_command's own return code
	// entirely (no check_rc), so a non-zero exit here still searches
	// whatever stdout it got — runStatus (not run) matches that
	// leniency, rather than treating a non-zero exit as a Go error.
	res, err := runStatus(ctx, conn, ctl+" -V")
	if err != nil {
		return false, err
	}
	return apache2ThreadedPattern.MatchString(res.Stdout), nil
}

// apache2ModuleEnabled reports whether identifier is currently loaded,
// per `<ctl> -M` — matching real _module_is_enabled exactly, including
// its ignore_configcheck-guarded tolerance of a non-zero exit (an
// mpm_*-related AH00534 error is swallowed silently when
// ignore_configcheck; any other error is swallowed only when
// ignore_configcheck too; otherwise a non-empty failMsg is returned).
func apache2ModuleEnabled(ctx context.Context, conn remoteexec.Connection, ctl, identifier, name string, ignoreConfigcheck bool) (enabled bool, failMsg string, err error) {
	res, err := runStatus(ctx, conn, ctl+" -M")
	if err != nil {
		return false, "", err
	}
	if res.RC != 0 {
		errMsg := fmt.Sprintf("Error executing %s: %s", ctl, res.Stderr)
		if !ignoreConfigcheck {
			return false, errMsg, nil
		}
		return false, "", nil
	}
	return strings.Contains(res.Stdout, " "+identifier), "", nil
}

// createApache2Identifier implements real create_apache_identifier:
// by a2enmod's own convention, a module loaded via `name` shows up in
// `apache2ctl -M` as name+"_module" — with a handful of exceptions for
// modules that don't follow that convention, checked in this exact
// order (a substring match on text_workarounds first, then a regex
// match on re_workarounds, falling through to the next candidate on a
// non-match rather than stopping, matching real code's own
// try/except-AttributeError/pass loop body).
func createApache2Identifier(name string) string {
	textWorkarounds := []struct{ spelling, module string }{
		{"shib", "mod_shib"},
		{"shib2", "mod_shib"},
		{"evasive", "evasive20_module"},
	}
	for _, w := range textWorkarounds {
		if strings.Contains(name, w.spelling) {
			return w.module
		}
	}

	reWorkarounds := []struct {
		search string
		re     *regexp.Regexp
	}{
		{"php8", regexp.MustCompile(`^(php)[\d.]+`)},
		{"php", regexp.MustCompile(`^(php\d)\.`)},
	}
	for _, w := range reWorkarounds {
		if strings.Contains(name, w.search) {
			if m := w.re.FindStringSubmatch(name); m != nil {
				return m[1] + "_module"
			}
		}
	}

	return name + "_module"
}
