package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePing implements Ansible's `ping` module: a trivial connectivity
// check that returns "pong" (or the given `data`) on success.
//
// Real ansible.builtin.ping proves nothing beyond "Python was
// successfully copied to and executed on the target" — it does no
// separate round-trip over the wire. This port has no analogous
// module-copy step (every module already reaches the target only
// through conn.Exec), so there is nothing extra to prove by that
// measure. For parity we still perform a cheap no-op exec (`:`, the
// shell no-op builtin) via conn before returning, so a broken
// connection surfaces as a `ping` failure the same way it would in
// real Ansible, even though the command itself does nothing.
//
// Args: data (string, default "pong") — echoed back in Extra["ping"].
// If data == "crash", fails deliberately, matching real
// ansible.builtin.ping's documented crash-testing behavior.
func modulePing(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	data := argString(args, "data", "pong")
	if data == "crash" {
		return Fail("boom"), nil
	}
	if _, err := run(ctx, conn, ":"); err != nil {
		return Result{}, err
	}
	return Ok(data).WithExtra("ping", data), nil
}
