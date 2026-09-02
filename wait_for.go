package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleWaitFor implements (a subset of) Ansible's `wait_for` module:
// polls the target until a port opens/closes or a path appears/
// disappears, or (with neither given) just sleeps for `timeout`.
//
// The poll loop is composed as a shell script and run once via
// conn.Exec, rather than as repeated Exec calls from the control node
// — a single command lets the target enforce its own timeout and
// avoids one round-trip per poll. The port-reachability check uses
// bash's `/dev/tcp/HOST/PORT` pseudo-device, which is a bash
// extension, not POSIX sh — this port therefore explicitly invokes
// `bash -c` for the whole script (rather than relying on whatever
// shell the connection's Exec happens to use by default), so it needs
// bash to be present on the target regardless of the connection's own
// default shell.
//
// Args: host (string, default "127.0.0.1"); port (int, optional); path
// (string, optional, mutually exclusive with port); timeout (int
// seconds, default 300); delay (int seconds, default 0); state
// (started|present|stopped|absent, default "started").
//
// Simplifications vs real wait_for: no search_regex, no
// active_connection_states/drained handling, no exclude_hosts, no
// connect_timeout distinct from the overall timeout.
func moduleWaitFor(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	host := argString(args, "host", "127.0.0.1")
	port := argInt(args, "port", 0)
	path := argString(args, "path", "")
	timeout := argInt(args, "timeout", 300)
	delay := argInt(args, "delay", 0)
	state := argString(args, "state", "started")

	cmd, err := waitForScript(host, port, path, timeout, delay, state)
	if err != nil {
		return Result{}, err
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	subject := waitForSubject(host, port, path)
	if res.RC != 0 {
		return Fail(fmt.Sprintf("Timeout when waiting for %s", subject)), nil
	}
	return Ok(subject), nil
}

// waitForScript builds the polling shell script for moduleWaitFor,
// separated out so its exact shape (and argument validation) can be
// asserted directly in tests.
func waitForScript(host string, port int, path string, timeout, delay int, state string) (string, error) {
	switch state {
	case "started", "present", "stopped", "absent":
	default:
		return "", errArg("wait_for: state must be started, present, stopped, or absent, got %q", state)
	}
	if port != 0 && path != "" {
		return "", errArg("wait_for: port and path are mutually exclusive")
	}
	wantPresent := state == "started" || state == "present"

	var cond string
	switch {
	case port != 0:
		cond = fmt.Sprintf("(exec 3<>/dev/tcp/%s/%d) 2>/dev/null", host, port)
	case path != "":
		cond = "test -e " + shellQuote(path)
	default:
		// No condition given: real wait_for just sleeps for `timeout`
		// and never errors in that case.
		return fmt.Sprintf("sleep %d", timeout), nil
	}

	want := 0
	if !wantPresent {
		want = 1
	}
	script := fmt.Sprintf(
		`if [ %d -gt 0 ]; then sleep %d; fi; end=$(( $(date +%%s) + %d )); `+
			`while true; do if %s; then r=0; else r=1; fi; `+
			`if [ "$r" -eq %d ]; then exit 0; fi; `+
			`if [ "$(date +%%s)" -ge "$end" ]; then exit 1; fi; sleep 1; done`,
		delay, delay, timeout, cond, want,
	)
	return "bash -c " + shellQuote(script), nil
}

func waitForSubject(host string, port int, path string) string {
	switch {
	case port != 0:
		return fmt.Sprintf("%s:%d", host, port)
	case path != "":
		return path
	default:
		return "timeout"
	}
}
