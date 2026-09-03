package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLaunchd implements (a subset of) Ansible's `launchd`
// (community.general) module: manages a macOS launchd service via
// `launchctl` — read from real launchd.py's own Plist/LaunchCtlTask
// class hierarchy (this batch's hard rule: the state machine driving
// which of load/unload/start/stop actually runs for each requested
// state, and the plist RunAtLoad/KeepAlive rewriting, are only visible
// there, not EXAMPLES/OPTIONS). Distinct from the Linux systemd/
// systemd_service/runit/monit modules already shipped in this package.
//
// Args: name (string, required) — the service's launchd label; plist
// (string, default "<name>.plist") — the job definition's filename,
// searched for (in order) under $HOME/Library/LaunchAgents,
// /Library/LaunchAgents, /Library/LaunchDaemons, /System/Library/
// LaunchAgents, /System/Library/LaunchDaemons; state (started|stopped|
// restarted|reloaded|unloaded, optional); enabled (bool, optional) —
// rewrites the plist's own RunAtLoad key; force_stop (bool, default
// false) — rewrites KeepAlive to false when stopping, so launchd
// doesn't immediately relaunch a KeepAlive job. At least one of state
// or enabled is required, matching real launchd's own
// required_one_of.
//
// This module ALWAYS requires the plist file to be found on the
// target, even for a bare state=started/stopped with neither enabled
// nor force_stop given — matching real launchd's own Plist.__init__,
// which looks the file up unconditionally (launchctl itself exposes no
// way to toggle RunAtLoad, so real launchd needs the file on disk
// regardless of whether this particular run happens to touch it).
//
// State detection: `launchctl list` output is tab-separated "pid\t
// last_exit_code\tlabel" rows (a header row is present too, but since
// its own three fields never equal a real label it never mixes into
// state detection, so it needs no special-casing here); a service not
// listed at all is "unloaded". For a listed service: an exit code
// outside {0,-2,-3,-9,-15} (real launchd's own documented "terminated
// by a signal" set, per launchctl's man page) is "unknown"; a non-"-"
// pid is "started"; otherwise "stopped". Real launchd's own
// line.split("\t") requires EXACTLY 3 fields and raises an unhandled
// exception on anything else; this port is more lenient and skips a
// malformed row instead of crashing — a deliberate, harmless deviation
// (see this package's general stance against reproducing a literal
// interpreter crash where a clean Fail/skip is available and behavior-
// equivalent for any well-formed target).
//
// Action logic per state is real launchd's own runCommand table:
//   - started: STOPPED (or the unreachable-in-practice LOADED) ->
//     reload()+start(); STARTED and the plist changed -> reload()+
//     start(); STARTED and unchanged -> no-op; UNLOADED -> load()+
//     start(); UNKNOWN -> reload()+start().
//   - stopped: STOPPED and the plist changed -> reload()+stop();
//     STOPPED and unchanged -> no-op; STARTED (or LOADED) -> reload()
//     first only if the plist changed, then always stop(); UNKNOWN ->
//     reload()+stop(); UNLOADED -> no-op (real launchd has no case for
//     it either — stopping an already-unloaded service is a no-op).
//   - reloaded: UNLOADED -> load() only (unloading an already-unloaded
//     service errors in real launchctl); anything else -> reload()
//     (unload()+load()).
//   - restarted: same as reloaded, then always start() afterward.
//   - unloaded: always unload(), regardless of current state.
//
// reload() is unload()+load(); load()/unload() run `launchctl load|
// unload <plist file>`; start()/stop() run `launchctl start|stop
// <name>`. A non-zero exit from any of these is a hard failure
// (Result{Failed:true}), matching real launchd's own module.fail_json
// for the same case.
//
// Simplifications vs real launchd: no artificial delay after start()/
// stop() — real launchd sleeps 5 seconds after each (launchctl itself
// returns before the job has actually finished starting/stopping, so
// real launchd pads for that), which this port does not reproduce:
// this architecture's Exec is already a blocking round trip per
// command, and a fixed sleep adds no correctness here while making
// every test of this module 5+ seconds slower for no observable
// difference on a real target (a caller wanting to observe the
// service's own eventual state can just poll `launchctl list` itself);
// this port has no check_mode concept (see this package's own Func
// doc comment — actions always run for real, there is no
// check-mode/predicted-changed distinction to draw here, matching this
// port's general architecture rather than a launchd-specific
// narrowing); RunAtLoad/KeepAlive are read/written via `plutil
// -convert json` round-trips over the target's own plist file (through
// conn.Exec, never a local file read — this port's Connection has no
// direct filesystem access to the target) rather than Python's
// plistlib; this works for both XML and binary plists (plutil handles
// both transparently) which is at least as general as real launchd's
// own plistlib-based reader. Both RunAtLoad and KeepAlive are read and
// rewritten from a SINGLE plutil JSON round trip (one read, up to one
// write) rather than real launchd's own two independent read-modify-
// write passes — a documented, behavior-preserving optimization in the
// same spirit as homectl.go's own consolidated calls, since both
// fields live in the same plist file read moments apart in the same
// module run.
func moduleLaunchd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	_, hasState := args["state"]
	state := argString(args, "state", "")
	if hasState {
		switch state {
		case "reloaded", "restarted", "started", "stopped", "unloaded":
		default:
			return Result{}, errArg("launchd: state must be one of reloaded, restarted, started, stopped, unloaded, got %q", state)
		}
	}
	_, hasEnabled := args["enabled"]
	enabled := argBool(args, "enabled", false)
	forceStop := argBool(args, "force_stop", false)
	if !hasState && !hasEnabled {
		return Result{}, errArg("launchd: at least one of state or enabled is required")
	}

	plistName := argString(args, "plist", name+".plist")

	prevState, prevPid, _, err := launchdGetState(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	plistPath, err := launchdFindPlist(ctx, conn, plistName)
	if err != nil {
		return Result{}, err
	}
	if plistPath == "" {
		msg := "launchd: unable to find the plist file " + plistName + " for service " + name
		if prevPid == "-" && prevState == "unloaded" {
			msg += " and it was not found among active services"
		}
		return Fail(msg), nil
	}

	plistChanged, err := launchdUpdatePlist(ctx, conn, plistPath, hasEnabled, enabled, forceStop)
	if err != nil {
		return Result{}, err
	}

	curState, curPid := prevState, prevPid
	if hasState {
		var failMsg string
		curState, curPid, failMsg, err = launchdRunAction(ctx, conn, state, name, plistPath, curState, plistChanged)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
	}

	changed := plistChanged
	if hasState && (state == "restarted" || state == "reloaded") {
		changed = true
	}
	if curState != prevState || curPid != prevPid {
		changed = true
	}

	return Changed("").WithExtra("status", map[string]any{
		"previous_state": prevState,
		"previous_pid":   prevPid,
		"current_state":  curState,
		"current_pid":    curPid,
	}).setChanged(changed), nil
}

// setChanged is a small chaining helper so moduleLaunchd can build its
// status Extra fields before deciding the final Changed bit (Ansible's
// changed/failed/msg triple isn't itself part of Extra, so WithExtra
// alone can't express it).
func (r Result) setChanged(changed bool) Result {
	r.Changed = changed
	return r
}

// launchdGetState runs `launchctl list` and reports name's own state/
// pid/last-exit-code, per moduleLaunchd's own doc comment. statusCode
// is "" if the service is not listed at all.
func launchdGetState(ctx context.Context, conn remoteexec.Connection, name string) (state, pid, statusCode string, err error) {
	out, err := run(ctx, conn, "launchctl list")
	if err != nil {
		return "", "", "", err
	}
	state, pid = "unloaded", "-"
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		if strings.TrimSpace(fields[2]) != name {
			continue
		}
		pid = fields[0]
		statusCode = fields[1]
		switch {
		case !launchdOKExit(statusCode):
			state = "unknown"
		case pid != "-":
			state = "started"
		default:
			state = "stopped"
		}
		break
	}
	return state, pid, statusCode, nil
}

func launchdOKExit(code string) bool {
	switch code {
	case "0", "-2", "-3", "-9", "-15":
		return true
	}
	return false
}

// launchdFindPlist searches the standard launchd job-definition
// directories for filename, in the order real launchd's own
// __find_service_plist checks them. Returns "" if not found in any of
// them.
func launchdFindPlist(ctx context.Context, conn remoteexec.Connection, filename string) (string, error) {
	home, err := run(ctx, conn, `printf '%s' "$HOME"`)
	if err != nil {
		return "", err
	}
	var dirs []string
	if home != "" {
		dirs = append(dirs, home+"/Library/LaunchAgents")
	}
	dirs = append(dirs,
		"/Library/LaunchAgents",
		"/Library/LaunchDaemons",
		"/System/Library/LaunchAgents",
		"/System/Library/LaunchDaemons",
	)
	for _, dir := range dirs {
		path := dir + "/" + filename
		exists, err := pathExists(ctx, conn, path)
		if err != nil {
			return "", err
		}
		if exists {
			return path, nil
		}
	}
	return "", nil
}

// launchdUpdatePlist applies the enabled (-> RunAtLoad) and force_stop
// (-> KeepAlive=false, only when force_stop is true AND KeepAlive is
// currently true) rules from moduleLaunchd's own doc comment, via one
// `plutil -convert json` read and up to one `plutil -convert xml1`
// write back to plistPath. Reports whether anything was written.
func launchdUpdatePlist(ctx context.Context, conn remoteexec.Connection, plistPath string, hasEnabled, enabled, forceStop bool) (bool, error) {
	out, err := run(ctx, conn, "plutil -convert json -o - "+shellQuote(plistPath))
	if err != nil {
		return false, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return false, err
	}
	if m == nil {
		m = map[string]any{}
	}

	changed := false
	if hasEnabled {
		runAtLoad, _ := m["RunAtLoad"].(bool)
		if runAtLoad != enabled {
			m["RunAtLoad"] = enabled
			changed = true
		}
	}
	if forceStop {
		keepAlive, _ := m["KeepAlive"].(bool)
		if keepAlive {
			m["KeepAlive"] = false
			changed = true
		}
	}
	if !changed {
		return false, nil
	}

	data, err := json.Marshal(m)
	if err != nil {
		return false, err
	}
	if _, err := conn.Exec(ctx, "plutil -convert xml1 -o "+shellQuote(plistPath)+" -", strings.NewReader(string(data))); err != nil {
		return false, err
	}
	return true, nil
}

// launchdRunAction executes the real launchd runCommand table for
// state (see moduleLaunchd's own doc comment) and returns the
// resulting state/pid, or a non-empty failMsg if any launchctl step
// failed (a hard failure — real launchd's own module.fail_json for the
// same case — which halts the sequence, matching real launchd's own
// module.fail_json-inside-a-helper-method short-circuit: a failed
// reload() never lets a subsequent start()/stop() run either).
func launchdRunAction(ctx context.Context, conn remoteexec.Connection, state, name, plistPath, curState string, plistChanged bool) (newState, newPid, failMsg string, err error) {
	// stepErr latches the first transport-level error from any step
	// below; once set, step() and reload() become no-ops (returning ""
	// without running anything further) so a transport failure partway
	// through a reload()+start() combo can never let the later call
	// run anyway — see this function's own doc comment.
	var stepErr error
	step := func(command, target string) string {
		if stepErr != nil {
			return ""
		}
		m, e := launchdCtl(ctx, conn, command, target)
		if e != nil {
			stepErr = e
			return ""
		}
		return m
	}
	reload := func() string {
		if m := step("unload", plistPath); m != "" {
			return m
		}
		if stepErr != nil {
			return ""
		}
		return step("load", plistPath)
	}
	start := func() string { return step("start", name) }
	stop := func() string { return step("stop", name) }
	load := func() string { return step("load", plistPath) }
	unload := func() string { return step("unload", plistPath) }

	switch state {
	case "started":
		switch curState {
		case "stopped", "unknown":
			if failMsg = reload(); failMsg == "" {
				failMsg = start()
			}
		case "started":
			if plistChanged {
				if failMsg = reload(); failMsg == "" {
					failMsg = start()
				}
			}
		case "unloaded":
			if failMsg = load(); failMsg == "" {
				failMsg = start()
			}
		}
	case "stopped":
		switch curState {
		case "stopped":
			if plistChanged {
				if failMsg = reload(); failMsg == "" {
					failMsg = stop()
				}
			}
		case "started":
			if plistChanged {
				failMsg = reload()
			}
			if failMsg == "" {
				failMsg = stop()
			}
		case "unknown":
			if failMsg = reload(); failMsg == "" {
				failMsg = stop()
			}
		}
	case "reloaded":
		if curState == "unloaded" {
			failMsg = load()
		} else {
			failMsg = reload()
		}
	case "restarted":
		if curState == "unloaded" {
			failMsg = load()
		} else {
			failMsg = reload()
		}
		if failMsg == "" {
			failMsg = start()
		}
	case "unloaded":
		failMsg = unload()
	}
	if stepErr != nil {
		return "", "", "", stepErr
	}
	if failMsg != "" {
		return "", "", failMsg, nil
	}

	newState, newPid, _, err = launchdGetState(ctx, conn, name)
	return newState, newPid, "", err
}

// launchdCtl runs `launchctl <command> <target>`. err is a genuine
// transport-level failure (propagated as a Go error, per this
// package's own Func doc comment); a non-zero exit is instead reported
// via a non-empty failMsg, matching real launchd's own module.fail_json
// wording for any launchctl invocation failure (an expected, well-
// formed failure, not a Go error — see this file's own doc comment).
func launchdCtl(ctx context.Context, conn remoteexec.Connection, command, target string) (failMsg string, err error) {
	res, err := runStatus(ctx, conn, "launchctl "+command+" "+shellQuote(target))
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "launchd: unable to " + command + " '" + target + "': " + strings.TrimSpace(res.Stderr), nil
	}
	return "", nil
}
