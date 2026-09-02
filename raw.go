package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRaw implements Ansible's `raw` module: runs a command directly
// through the target's shell, with no module-wrapping at all.
//
// In real Ansible, raw's whole reason to exist is that it (like
// script) requires no Python on the target, unlike every other module
// which normally gets assembled into a Python script and copied over —
// that distinction is meaningless in this port, since NO module in
// this package has ever needed a target-side interpreter: every module
// here, including command and shell, already runs on the control node
// and reaches the target only through conn.Exec/Put/Fetch (see
// module.go's package doc comment). So this port's raw is a thin
// near-duplicate of moduleShell's core logic (run cmdStr through
// conn.Exec, no argv-tokenizing) — the two are behaviorally identical
// here, which is a real, documented flattening of a distinction real
// Ansible cares about deeply and this architecture makes moot.
//
// Args: cmd (or `_raw_params`, matching real raw's free_form parameter
// name as it's normally passed) — the command line, required. Unlike
// moduleCommand/moduleShell, real ansible.builtin.raw's argspec has no
// chdir/creates/removes at all, so this port doesn't accept them
// either — passing them is silently ignored, not an error, since a
// caller migrating a shell task to raw by habit shouldn't get a
// surprising argument-validation failure.
func moduleRaw(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cmdStr := argString(args, "cmd", argString(args, "_raw_params", ""))
	if cmdStr == "" {
		return Result{}, errArg("raw: missing required argument: cmd")
	}
	res, err := conn.Exec(ctx, cmdStr, nil)
	if err != nil {
		return Result{}, err
	}
	return commandResult([]string{cmdStr}, res), nil
}
