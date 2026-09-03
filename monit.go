package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMonit implements Ansible's `monit` (community.general) module:
// manages a program monitored by the Monit daemon via the `monit` CLI,
// idempotent on `monit status <name>`'s own reported status — the same
// query-then-act shape as supervisorctl.go's module, but sourced from
// monit's own status block instead of supervisorctl's status column.
//
// Args: name (string, required); state (string, required — one of
// present, started, stopped, restarted, monitored, unmonitored,
// reloaded; real monit's own doc lists no default, so this port
// requires it rather than guessing one); timeout (int, default 300,
// seconds) — accepted for argspec compatibility with real monit, which
// polls up to this long for a pending action to finish; not
// implemented here (see below).
//
// State semantics, matching real monit's own documented behavior:
//   - present: `monit reload` (make monit re-read its config, picking
//     up a newly-added program) then, if the program still isn't known
//     to monit, fails — real monit can't summon a program into being;
//     it can only get monit to notice one already configured.
//   - started/stopped: `monit start <name>`/`monit stop <name>` if the
//     process's current status isn't already Running/Not monitored
//     respectively.
//   - restarted/reloaded: `monit restart <name>`/`monit reload`,
//     always reporting Changed — matching this port's general
//     unconditional-action-verb convention (see supervisorctl.go's own
//     doc comment) since monit's own restart has no separate "already
//     restarted" concept to check idempotency against.
//   - monitored/unmonitored: `monit monitor <name>`/`monit unmonitor
//     <name>` if not already in that monitoring state.
//
// Simplifications vs real monit: no polling loop against `timeout` —
// real monit's own implementation repeatedly re-checks `monit status`
// for up to `timeout` seconds (sleeping 5s between checks) waiting for
// a pending action to actually apply; this port issues the one command
// and reports the outcome immediately, since `monit` itself already
// blocks its own CLI invocations on the daemon's actual state change in
// the common case, and a full poll-with-timeout loop was judged low
// value for the added complexity in this batch.
func moduleMonit(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		if _, err := run(ctx, conn, "monit reload"); err != nil {
			return Result{}, err
		}
		_, exists, err := monitStatus(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail(name + " not present in monit after reload"), nil
		}
		return Changed(name + " present"), nil

	case "started":
		status, exists, err := monitStatus(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail(name + ": not monitored by monit"), nil
		}
		if strings.Contains(status, "running") {
			return Ok(name + " already started"), nil
		}
		if _, err := run(ctx, conn, "monit start "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " started"), nil

	case "stopped":
		status, exists, err := monitStatus(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail(name + ": not monitored by monit"), nil
		}
		if strings.Contains(status, "not monitored") {
			return Ok(name + " already stopped"), nil
		}
		if _, err := run(ctx, conn, "monit stop "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " stopped"), nil

	case "restarted":
		if _, err := run(ctx, conn, "monit restart "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " restarted"), nil

	case "reloaded":
		if _, err := run(ctx, conn, "monit reload"); err != nil {
			return Result{}, err
		}
		return Changed("monit reloaded"), nil

	case "monitored":
		status, exists, err := monitStatus(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail(name + ": not monitored by monit"), nil
		}
		if !strings.Contains(status, "not monitored") {
			return Ok(name + " already monitored"), nil
		}
		if _, err := run(ctx, conn, "monit monitor "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " monitored"), nil

	case "unmonitored":
		status, exists, err := monitStatus(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail(name + ": not monitored by monit"), nil
		}
		if strings.Contains(status, "not monitored") {
			return Ok(name + " already unmonitored"), nil
		}
		if _, err := run(ctx, conn, "monit unmonitor "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " unmonitored"), nil

	default:
		return Result{}, errArg("monit: state must be one of present, started, stopped, restarted, "+
			"monitored, unmonitored, reloaded, got %q", state)
	}
}

// monitStatus runs `monit status <name>` and reports its lowercased
// "status" line's value and whether the process is known to monit at
// all (RC 0 and the process block is found in the output).
func monitStatus(ctx context.Context, conn remoteexec.Connection, name string) (status string, exists bool, err error) {
	res, err := conn.Exec(ctx, "monit status "+shellQuote(name), nil)
	if err != nil {
		return "", false, err
	}
	out := strings.ToLower(res.Stdout)
	if res.RC != 0 || strings.Contains(out, "not found") || strings.TrimSpace(out) == "" {
		return "", false, nil
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status") {
			_, v, ok := strings.Cut(line, "status")
			if ok {
				return strings.TrimSpace(v), true, nil
			}
		}
	}
	return "", true, nil
}
