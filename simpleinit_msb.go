package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSimpleinitMsb implements Ansible's `simpleinit_msb`
// (community.general) module: controls a service under Source Mage
// GNU/Linux's `simpleinit-msb` init system, via its own `telinit`
// front-end — a direct port (no CLI substitution needed: real
// simpleinit_msb.py already shells out to `telinit` itself via
// module.run_command()/daemonize(), exactly as this port does via
// simpleinitRun/conn.Exec), with one disclosed exception: see below on
// service_control's own fire-and-forget daemonize() call.
//
// Args: name (string, required, alias service); state (running|
// started|stopped|restarted|reloaded, optional); enabled (bool,
// optional); at least one of state/enabled is required, matching real
// simpleinit_msb.py's own required_one_of.
//
// telinit itself is located first (PATH, then /sbin, /usr/sbin, /bin,
// /usr/bin, matching real get_service_tools()'s own search dirs) and
// /etc/init.d/smgl_init's own existence is checked (real
// simpleinit_msb.py treats telinit as usable ONLY when that marker file
// exists, i.e. only ON a Source Mage host); either missing is
// Result{Failed:true} with real get_service_tools()'s own exact message
// ("cannot find telinit script for simpleinit-msb, aborting..."), not a
// Go error.
//
// enabled is applied first (via `telinit bootenable|bootdisable <name>`)
// when set, using real service_enabled()'s own check to decide whether
// it's already in the desired state. That check is ported VERBATIM from
// real simpleinit_msb.py, including what reads as an actual upstream
// bug: service_enabled() runs `telinit Trued` or `telinit Falsed`
// (literally Python's `str(self.enable) + "d"`) as telinit's own
// subcommand, rather than any sensibly-named enabled-query subcommand —
// this port intentionally does NOT silently correct that, per this
// project's own "read the reference source, replicate faithfully,
// document deviations rather than inventing fixes" rule; if this
// behavior is wrong on a real target, it was already wrong in upstream
// community.general. Similarly, the `bootenable`/`bootdisable` command's
// own exit code is never actually checked for failure by real
// simpleinit_msb.py (only its stderr text is scanned for "already
// enabled"/"already disabled" to decide whether anything actually
// changed) — ported the same way here.
//
// state (when given) drives running state via `telinit run <name>
// <action>`, matching real check_service_changed()/modify_service_state()
// exactly: started/running are idempotent (only act if not already
// running); stopped is idempotent (only act if running); restarted
// ALWAYS acts; reloaded ALWAYS acts too (starting the service if it
// isn't running yet, reloading it otherwise — matching real
// simpleinit_msb.py's own documented "reloaded starts the service if it
// is not already started"). One deliberate, disclosed deviation: real
// service_control() runs the actual start/stop/restart/reload/status
// command through daemonize() — a detached, non-blocking child process
// whose real exit code the real module never reliably observes (a
// second, structural upstream quirk, not merely a Python-ism this port
// could trivially replicate: remoteexec.Connection's own Exec is
// synchronous and has no fork-and-detach primitive). This port instead
// runs that command synchronously and DOES check its exit code
// (Result{Failed:true} on a non-zero exit from the actual state-change
// command) — a stricter, arguably more correct behavior than upstream's
// own fire-and-forget approach, not an attempt to fake non-blocking
// semantics it cannot actually provide.
//
// Service existence (`telinit list`, matching a `^\w+\s+<name>$` line)
// is checked before every telinit invocation that names a specific
// service, matching real service_exists()'s own repeated calls exactly;
// an unknown service is Result{Failed:true} with real
// simpleinit_msb.py's own exact message.
func moduleSimpleinitMsb(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", argString(args, "service", ""))
	if name == "" {
		return Result{}, errArg("simpleinit_msb: missing required argument: name (or its alias service)")
	}
	_, stateSet := args["state"]
	state := argString(args, "state", "")
	if stateSet {
		switch state {
		case "running", "started", "stopped", "restarted", "reloaded":
		default:
			return Result{}, errArg("simpleinit_msb: state must be one of running, started, stopped, restarted, reloaded, got %q", state)
		}
	}
	_, enabledSet := args["enabled"]
	enabledWant := argBool(args, "enabled", false)
	if !stateSet && !enabledSet {
		return Result{}, errArg("simpleinit_msb: one of state or enabled is required")
	}

	telinit, failMsg, err := simpleinitFindTelinit(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	changed := false
	res := Result{}
	res = res.WithExtra("name", name)

	if enabledSet {
		c, failMsg, err := simpleinitServiceEnable(ctx, conn, telinit, name, enabledWant)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		changed = changed || c
		res = res.WithExtra("enabled", enabledWant)
	}

	if !stateSet {
		res.Changed = changed
		return res, nil
	}
	res = res.WithExtra("state", state)

	running, failMsg, err := simpleinitServiceStatus(ctx, conn, telinit, name)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}
	if running == nil {
		return Fail("failed determining service state, possible typo of service name?"), nil
	}

	svcChange := false
	switch {
	case !*running && (state == "started" || state == "running" || state == "reloaded"):
		svcChange = true
	case *running && (state == "stopped" || state == "reloaded"):
		svcChange = true
	case state == "restarted":
		svcChange = true
	}

	if svcChange {
		action := ""
		switch {
		case state == "started" || state == "running":
			action = "start"
		case !*running && state == "reloaded":
			action = "start"
		case state == "stopped":
			action = "stop"
		case state == "reloaded":
			action = "reload"
		case state == "restarted":
			action = "restart"
		}
		if action != "" {
			rc, stdout, stderr, failMsg, err := simpleinitServiceControl(ctx, conn, telinit, name, action)
			if err != nil {
				return Result{}, err
			}
			if failMsg != "" {
				return Fail(failMsg), nil
			}
			if rc != 0 {
				msg := strings.TrimSpace(stderr)
				if msg == "" {
					msg = strings.TrimSpace(stdout)
				}
				return Fail(fmt.Sprintf("simpleinit_msb: %s %s: %s", action, name, msg)), nil
			}
		}
	}
	changed = changed || svcChange

	resultState := "stopped"
	switch state {
	case "started", "restarted", "running", "reloaded":
		resultState = "started"
	}
	res.Changed = changed
	res = res.WithExtra("state", resultState)
	return res, nil
}

func simpleinitRun(ctx context.Context, conn remoteexec.Connection, telinit string, argv []string) (remoteexec.Result, error) {
	full := append([]string{telinit}, argv...)
	quoted := make([]string, len(full))
	for i, a := range full {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, strings.Join(quoted, " "), nil)
}

func simpleinitFindTelinit(ctx context.Context, conn remoteexec.Connection) (path, failMsg string, err error) {
	if out, runErr := run(ctx, conn, "command -v telinit"); runErr == nil && strings.TrimSpace(out) != "" {
		path = strings.TrimSpace(out)
	} else {
		for _, dir := range []string{"/sbin", "/usr/sbin", "/bin", "/usr/bin"} {
			candidate := dir + "/telinit"
			exists, existsErr := pathExists(ctx, conn, candidate)
			if existsErr != nil {
				return "", "", existsErr
			}
			if exists {
				path = candidate
				break
			}
		}
	}
	if path == "" {
		return "", "cannot find telinit script for simpleinit-msb, aborting...", nil
	}
	exists, err := pathExists(ctx, conn, "/etc/init.d/smgl_init")
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", "cannot find telinit script for simpleinit-msb, aborting...", nil
	}
	return path, "", nil
}

func simpleinitServiceExists(ctx context.Context, conn remoteexec.Connection, telinit, name string) (bool, error) {
	res, err := simpleinitRun(ctx, conn, telinit, []string{"list"})
	if err != nil {
		return false, err
	}
	re := regexp.MustCompile(`^\w+\s+` + regexp.QuoteMeta(name) + `$`)
	for _, line := range splitLines(res.Stdout) {
		if re.MatchString(line) {
			return true, nil
		}
	}
	return false, nil
}

// simpleinitServiceControl runs `telinit run <name> <action>` — see
// moduleSimpleinitMsb's own doc comment for why this port runs it
// synchronously (and checks its exit code) rather than replicating real
// service_control()'s own daemonize()-based fire-and-forget dispatch.
func simpleinitServiceControl(ctx context.Context, conn remoteexec.Connection, telinit, name, action string) (rc int, stdout, stderr, failMsg string, err error) {
	exists, err := simpleinitServiceExists(ctx, conn, telinit, name)
	if err != nil {
		return 0, "", "", "", err
	}
	if !exists {
		return 0, "", "", fmt.Sprintf("telinit could not find the requested service: %s", name), nil
	}
	res, err := simpleinitRun(ctx, conn, telinit, []string{"run", name, action})
	if err != nil {
		return 0, "", "", "", err
	}
	return res.RC, res.Stdout, res.Stderr, "", nil
}

// simpleinitServiceStatus runs the "status" action and parses its
// output exactly like real get_service_status(): only when the output
// is at most one line long does this port look for "is not running" /
// "is running" (case-insensitively, with the service's own name
// stripped out first) to decide the result; anything else (including a
// status command that failed outright) leaves running nil, matching
// real simpleinit_msb.py's own "possible typo" ambiguity exactly.
func simpleinitServiceStatus(ctx context.Context, conn remoteexec.Connection, telinit, name string) (running *bool, failMsg string, err error) {
	_, stdout, _, failMsg, err := simpleinitServiceControl(ctx, conn, telinit, name, "status")
	if err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	if strings.Count(stdout, "\n") > 1 {
		return nil, "", nil
	}
	cleanout := strings.ReplaceAll(strings.ToLower(stdout), strings.ToLower(name), "")
	switch {
	case strings.Contains(cleanout, "is not running"):
		f := false
		return &f, "", nil
	case strings.Contains(cleanout, "is running"):
		t := true
		return &t, "", nil
	}
	return nil, "", nil
}

// simpleinitServiceEnabledCheck is a faithful, VERBATIM port of real
// simpleinit_msb.py's own service_enabled() — see moduleSimpleinitMsb's
// own doc comment for why its "telinit Trued"/"telinit Falsed" command
// construction is preserved rather than corrected.
func simpleinitServiceEnabledCheck(ctx context.Context, conn remoteexec.Connection, telinit, name string, enable bool) (enabled bool, failMsg string, err error) {
	exists, err := simpleinitServiceExists(ctx, conn, telinit, name)
	if err != nil {
		return false, "", err
	}
	if !exists {
		return false, fmt.Sprintf("telinit could not find the requested service: %s", name), nil
	}
	verb := "Falsed"
	if enable {
		verb = "Trued"
	}
	res, err := simpleinitRun(ctx, conn, telinit, []string{verb})
	if err != nil {
		return false, "", err
	}
	serviceEnabled := !enable
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `$`)
	for _, line := range splitLines(res.Stdout) {
		if re.MatchString(line) {
			serviceEnabled = enable
			break
		}
	}
	return serviceEnabled, "", nil
}

func simpleinitServiceEnable(ctx context.Context, conn remoteexec.Connection, telinit, name string, enable bool) (changed bool, failMsg string, err error) {
	already, failMsg, err := simpleinitServiceEnabledCheck(ctx, conn, telinit, name, enable)
	if err != nil {
		return false, "", err
	}
	if failMsg != "" {
		return false, failMsg, nil
	}
	if already == enable {
		return false, "", nil
	}
	action := "bootdisable"
	if enable {
		action = "bootenable"
	}
	res, err := simpleinitRun(ctx, conn, telinit, []string{action, name})
	if err != nil {
		return false, "", err
	}
	changed = true
	for _, line := range splitLines(res.Stderr) {
		if enable && strings.Contains(line, "already enabled") {
			changed = false
			break
		}
		if !enable && strings.Contains(line, "already disabled") {
			changed = false
			break
		}
	}
	return changed, "", nil
}
