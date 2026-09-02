package modules

import (
	"context"
	"fmt"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleWaitForConnection implements (a subset of) Ansible's
// `wait_for_connection` module: polls until the connection ITSELF is
// usable — unlike wait_for.go's `wait_for`, which polls for a port or
// path on an already-working connection.
//
// Real wait_for_connection repeatedly tears down and re-establishes
// its own transport connection (SSH/WinRM/etc.), which is exactly the
// scenario this needs to handle (a target mid-boot, or right after a
// `reboot` task, may refuse or reset connections entirely). This port
// cannot do that: a module here is handed one already-connected
// remoteexec.Connection and has no way to ask for a fresh one — only
// the playbook engine dials connections (see moduleReboot's doc
// comment for the same structural gap). The simplest HONEST
// implementation available at a single module's level: retry a trivial
// `true` command on the connection ALREADY HELD, in a loop, until it
// succeeds or the timeout elapses. This only actually detects "the
// existing connection recovered" (e.g. an SSH multiplexed session that
// silently reconnects underneath), NOT "a freshly dialed connection to
// a target that finished rebooting" — if the underlying transport
// doesn't itself retry/reconnect, this loop will just keep failing the
// same way until it times out, which is a real, documented gap versus
// real wait_for_connection's behavior.
//
// Args: connect_timeout (int, default 5) — accepted for
// shape-compatibility with real wait_for_connection's argspec, but
// unused: Connection.Exec has no per-call timeout knob distinct from
// ctx's own deadline, so there is nothing to apply this to. delay (int,
// default 0) — seconds to wait before the first attempt. sleep (int,
// default 1) — seconds between attempts. timeout (int, default 600) —
// overall deadline in seconds.
func moduleWaitForConnection(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	delay := argInt(args, "delay", 0)
	sleep := argInt(args, "sleep", 1)
	timeout := argInt(args, "timeout", 600)

	if delay > 0 {
		if err := sleepCtx(ctx, delay); err != nil {
			return Result{}, err
		}
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		res, err := conn.Exec(ctx, "true", nil)
		if err == nil && res.RC == 0 {
			return Ok("connection is usable"), nil
		}
		if !time.Now().Before(deadline) {
			return Fail(fmt.Sprintf("wait_for_connection: timed out after %ds waiting for a usable connection", timeout)), nil
		}
		if err := sleepCtx(ctx, sleep); err != nil {
			return Result{}, err
		}
	}
}

// sleepCtx sleeps for seconds, returning ctx.Err() if ctx is canceled
// first (rather than sleeping obliviously past a caller's own
// deadline/cancellation).
func sleepCtx(ctx context.Context, seconds int) error {
	if seconds <= 0 {
		return nil
	}
	select {
	case <-time.After(time.Duration(seconds) * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
