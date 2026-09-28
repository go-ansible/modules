package modules

import (
	"context"
	"fmt"
	"time"

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

	started := time.Now()
	cmd, err := waitForScript(host, port, path, timeout, delay, state)
	if err != nil {
		return Result{}, err
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	subject := waitForSubject(host, port, path)

	// Real's own exit_json names exactly these seven, on success and
	// (minus most of them) on the timeout paths, which all carry
	// elapsed:
	//
	//	state port search_regex match_groups match_groupdict path elapsed
	//
	// This port returned a msg and nothing else, so `wait_for` could
	// be registered but not READ -- and add_path_info, which real
	// applies to any result carrying `path`, had nothing to key on
	// either. Measured against ansible-core 2.21.4: an existing path
	// gives thirteen keys, nine of them from here and four more from
	// the framework.
	//
	// match_groups and match_groupdict are always present and always
	// EMPTY here: search_regex is not implemented by this port, so
	// there is never a match to report. Real defaults them to () and
	// {} and only fills them when search_regex matched, so the shape
	// agrees and the values say "no match" on both sides -- which is
	// true here for the honest reason that nothing was searched.
	withKeys := func(r Result) Result {
		return r.WithExtra("state", state).
			// port and search_regex have NO default in real's argument
			// spec -- `port=dict(type='int')`, `search_regex=dict(
			// type='str')` -- so an unset one is None, not 0 and not
			// "". Measured: the corpus caught `port=0` against real's
			// `port=`.
			WithExtra("port", argOrNil(args, "port", port)).
			WithExtra("search_regex", argOrNil(args, "search_regex", nil)).
			WithExtra("match_groups", []any{}).
			WithExtra("match_groupdict", map[string]any{}).
			WithExtra("path", path).
			WithExtra("elapsed", elapsedSeconds(started))
	}
	if res.RC != 0 {
		// The timeout path DOES carry a msg: real's fail_json names
		// one on every one of its own timeout branches.
		return withKeys(Fail(fmt.Sprintf("Timeout when waiting for %s", subject))), nil
	}
	// The success path carries none. Real's exit_json lists seven keys
	// and msg is not among them.
	ok := Ok(subject)
	ok.NoMsg = true
	return withKeys(ok), nil
}

// elapsedSeconds is real's own `elapsed`: whole seconds, truncated,
// from datetime's .seconds field.
func elapsedSeconds(since time.Time) int {
	d := int(time.Since(since).Seconds())
	if d < 0 {
		return 0
	}
	return d
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

// argOrNil returns the parsed value when the caller gave the argument
// and nil when it did not -- real's own "no default in the argument
// spec means None" for a module that reports the argument back.
func argOrNil(args map[string]any, key string, parsed any) any {
	if _, ok := args[key]; !ok {
		return nil
	}
	return parsed
}
