package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePuppet implements Ansible's `puppet` (community.general)
// module: runs `puppet agent --onetime ...` (or, when manifest/execute
// is given, `puppet apply ...`) with `--detailed-exitcodes`, and maps
// its exit code onto changed/failed/msg — read from real puppet.py and
// its module_utils/_puppet.py (this batch's hard rule: read the
// reference implementation before implementing, not just ansible-doc's
// option list, since the exit-code mapping below is not documented in
// EXAMPLES/OPTIONS at all).
//
// Args: puppetmaster (string) — mutually exclusive with manifest,
// execute, and modulepath (matching real puppet's own
// mutually_exclusive groups); modulepath (string); manifest (string) —
// a path that MUST already exist on the target, checked before running
// anything (a missing manifest fails cleanly, matching real puppet's
// own module.fail_json for this case) — giving manifest or execute
// selects `puppet apply` instead of `puppet agent`; confdir, certname,
// environment (string); tags, skip_tags ([]string, joined with `,`);
// execute (string) — Puppet code to run inline via `apply --execute`;
// noop (bool, optional — tri-state: unset means "don't pass either
// --noop or --no-noop, use puppet.conf's own default", matching real
// puppet's own documented default behavior); use_srv_records (bool,
// optional, agent mode only); logdest (stdout|syslog|all, default
// "stdout" — "stdout" passes no flag, since real puppet apply's own
// default log destination already is stdout); waitforlock (string);
// summarize, debug, verbose, show_diff (bool, default false); timeout
// (string, default "30m") — when non-empty, the whole command is
// wrapped in `timeout -s 9 <timeout>` (this port always assumes a
// `timeout(1)` binary is present on the target when this is set,
// unlike real puppet's own module, which probes for it first and
// silently skips the wrapper if absent); environment_lang (string,
// default "C") — prefixes the command with `LANG=<value>`, except for
// the value "auto", which this port treats as equivalent to unset (no
// LANG prefix) rather than replicating real puppet's own auto-detection
// of a suitable installed locale; facts (map, optional) and
// facter_basename (string, default "ansible") — written as a JSON file
// via facter's own external-facts directory convention (see
// puppetFacterDir).
//
// Exit-code handling (from real puppet.py's own main(), verbatim —
// including the parts that read as surprising): with
// --detailed-exitcodes, puppet agent/apply use 0=no changes,
// 1=error/disabled, 2=changes made, 124=timed out (only reachable via
// the `timeout` wrapper above), anything else=failure. Real puppet's
// own module maps these to:
//   - rc==0: Ok, unchanged.
//   - rc==2: Changed — NOT failed.
//   - rc==1: real puppet.py calls module.exit_json (NOT fail_json) here
//     — so despite being puppet's own "error" exit code, the real
//     module reports task SUCCESS (unchanged), just with an extra
//     "error": true field and a msg ("puppet is disabled" if stdout
//     contains "administratively disabled", else "puppet did not
//     run"). This looks like it should be a failure and is not; this
//     port replicates that faithfully rather than silently "fixing" it
//     into a Failed result, per this project's iron rule against
//     deviating from real (if surprising) behavior undocumented.
//   - rc==124: also exit_json, not fail_json — unchanged, msg notes the
//     timeout, not Failed either.
//   - any other rc: fail_json — Failed, with msg "... failed with
//     return code: N".
//
// Before running the agent-mode command (manifest and execute both
// unset), real puppet.py's own ensure_agent_enabled runs `puppet config
// print agent_disabled_lockfile` and fails cleanly (Result{Failed:true})
// if that lockfile path exists on the target ("Puppet agent is
// administratively disabled.") or if the config-print command itself
// doesn't succeed.
func modulePuppet(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	timeout := argString(args, "timeout", "30m")
	puppetmaster := argString(args, "puppetmaster", "")
	modulepath := argString(args, "modulepath", "")
	manifest := argString(args, "manifest", "")
	confdir := argString(args, "confdir", "")
	environment := argString(args, "environment", "")
	certname := argString(args, "certname", "")
	tags := argStringList(args, "tags")
	skipTags := argStringList(args, "skip_tags")
	execute := argString(args, "execute", "")
	logdest := argString(args, "logdest", "stdout")
	waitforlock := argString(args, "waitforlock", "")
	envLang := argString(args, "environment_lang", "C")
	summarize := argBool(args, "summarize", false)
	debug := argBool(args, "debug", false)
	verbose := argBool(args, "verbose", false)
	showDiff := argBool(args, "show_diff", false)

	if puppetmaster != "" && (manifest != "" || execute != "" || modulepath != "") {
		return Result{}, errArg("puppet: puppetmaster is mutually exclusive with manifest, execute, and modulepath")
	}

	if manifest != "" {
		exists, err := pathExists(ctx, conn, manifest)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail("Manifest file " + manifest + " not found."), nil
		}
	}

	if manifest == "" && execute == "" {
		if failResult, err := puppetEnsureAgentEnabled(ctx, conn); err != nil {
			return Result{}, err
		} else if failResult != nil {
			return *failResult, nil
		}
	}

	if factsArg, ok := args["facts"]; ok {
		facts, ok2 := factsArg.(map[string]any)
		if !ok2 {
			return Result{}, errArg("puppet: facts must be a dict")
		}
		if len(facts) > 0 {
			if err := puppetWriteFacts(ctx, conn, facts, argString(args, "facter_basename", "ansible")); err != nil {
				return Result{}, err
			}
		}
	}

	var sub []string
	if manifest == "" && execute == "" {
		sub = []string{"agent", "--onetime", "--no-daemonize", "--no-usecacheonfailure", "--no-splay",
			"--detailed-exitcodes", "--verbose", "--color", "0"}
		if puppetmaster != "" {
			sub = append(sub, "--server", puppetmaster)
		}
		if showDiff {
			sub = append(sub, "--show-diff")
		}
		if confdir != "" {
			sub = append(sub, "--confdir", confdir)
		}
		if environment != "" {
			sub = append(sub, "--environment", environment)
		}
		if len(tags) > 0 {
			sub = append(sub, "--tags", strings.Join(tags, ","))
		}
		if len(skipTags) > 0 {
			sub = append(sub, "--skip_tags", strings.Join(skipTags, ","))
		}
		if certname != "" {
			sub = append(sub, "--certname="+certname)
		}
		if _, ok := args["noop"]; ok {
			sub = append(sub, puppetBoolFlag(argBool(args, "noop", false), "--noop", "--no-noop"))
		}
		if _, ok := args["use_srv_records"]; ok {
			sub = append(sub, puppetBoolFlag(argBool(args, "use_srv_records", false), "--usr_srv_records", "--no-usr_srv_records"))
		}
		if waitforlock != "" {
			sub = append(sub, "--waitforlock", waitforlock)
		}
	} else {
		sub = []string{"apply", "--detailed-exitcodes"}
		switch logdest {
		case "syslog":
			sub = append(sub, "--logdest", "syslog")
		case "all":
			sub = append(sub, "--logdest", "syslog", "--logdest", "console")
		}
		if modulepath != "" {
			sub = append(sub, "--modulepath="+modulepath)
		}
		if environment != "" {
			sub = append(sub, "--environment", environment)
		}
		if certname != "" {
			sub = append(sub, "--certname="+certname)
		}
		if len(tags) > 0 {
			sub = append(sub, "--tags", strings.Join(tags, ","))
		}
		if len(skipTags) > 0 {
			sub = append(sub, "--skip_tags", strings.Join(skipTags, ","))
		}
		if _, ok := args["noop"]; ok {
			sub = append(sub, puppetBoolFlag(argBool(args, "noop", false), "--noop", "--no-noop"))
		}
		if execute != "" {
			sub = append(sub, "--execute", execute)
		} else {
			sub = append(sub, manifest)
		}
		if summarize {
			sub = append(sub, "--summarize")
		}
		if debug {
			sub = append(sub, "--debug")
		}
		if verbose {
			sub = append(sub, "--verbose")
		}
		if waitforlock != "" {
			sub = append(sub, "--waitforlock", waitforlock)
		}
	}

	base := []string{"puppet"}
	if timeout != "" {
		base = []string{"timeout", "-s", "9", timeout, "puppet"}
	}
	full := append(append([]string{}, base...), sub...)

	quoted := make([]string, len(full))
	for i, tok := range full {
		quoted[i] = shellQuote(tok)
	}
	cmdLine := strings.Join(quoted, " ")
	if envLang != "" && envLang != "auto" {
		cmdLine = "LANG=" + shellQuote(envLang) + " " + cmdLine
	}

	res, err := runStatus(ctx, conn, cmdLine)
	if err != nil {
		return Result{}, err
	}
	rc, stdout, stderr := res.RC, res.Stdout, res.Stderr

	switch rc {
	case 0:
		return Ok("puppet: no changes").WithExtra("rc", 0).WithExtra("stdout", stdout).WithExtra("stderr", stderr), nil
	case 2:
		return Changed("puppet: changes applied").WithExtra("rc", 0).WithExtra("stdout", stdout).WithExtra("stderr", stderr), nil
	case 1:
		disabled := strings.Contains(stdout, "administratively disabled")
		msg := "puppet did not run"
		if disabled {
			msg = "puppet is disabled"
		}
		return Ok(msg).WithExtra("rc", rc).WithExtra("error", true).WithExtra("disabled", disabled).
			WithExtra("stdout", stdout).WithExtra("stderr", stderr), nil
	case 124:
		return Ok(cmdLine+" timed out").WithExtra("rc", rc).WithExtra("stdout", stdout).WithExtra("stderr", stderr), nil
	default:
		return Fail(cmdLine+": failed with return code: "+strconv.Itoa(rc)).WithExtra("rc", rc).
			WithExtra("stdout", stdout).WithExtra("stderr", stderr), nil
	}
}

func puppetBoolFlag(v bool, ifTrue, ifFalse string) string {
	if v {
		return ifTrue
	}
	return ifFalse
}

// puppetEnsureAgentEnabled implements real puppet.py's own
// ensure_agent_enabled: fails cleanly if puppet agent is
// administratively disabled (or if that can't be determined), returning
// a non-nil *Result only in that failure case.
func puppetEnsureAgentEnabled(ctx context.Context, conn remoteexec.Connection) (*Result, error) {
	res, err := runStatus(ctx, conn, "puppet config print agent_disabled_lockfile")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		r := Fail("Puppet agent state could not be determined.")
		return &r, nil
	}
	lockfile := strings.TrimSpace(res.Stdout)
	exists, err := pathExists(ctx, conn, lockfile)
	if err != nil {
		return nil, err
	}
	if exists {
		r := Fail("Puppet agent is administratively disabled.").WithExtra("disabled", true)
		return &r, nil
	}
	return nil, nil
}

// puppetWriteFacts writes facts as JSON to facter's own external-facts
// directory (get_facter_dir() in real _puppet.py: /etc/facter/facts.d
// for uid 0, else ~/.facter/facts.d), under <facter_basename>.json.
func puppetWriteFacts(ctx context.Context, conn remoteexec.Connection, facts map[string]any, basename string) error {
	uid, err := run(ctx, conn, "id -u")
	if err != nil {
		return err
	}
	dir := "/etc/facter/facts.d"
	if strings.TrimSpace(uid) != "0" {
		home, err := run(ctx, conn, "echo $HOME")
		if err != nil {
			return err
		}
		dir = strings.TrimSpace(home) + "/.facter/facts.d"
	}
	data, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	return writeRemote(ctx, conn, dir+"/"+basename+".json", data)
}
