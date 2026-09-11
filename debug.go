package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDebug implements Ansible's `debug` module: prints a message
// (or the value of `var`) without changing anything.
//
// Args: msg (string, default "Hello world!"); var (any) — a value to
// print by its own repr instead of a fixed message.
// moduleDebug handles the msg: form. The var: form is resolved by the
// playbook engine before it ever reaches here, because `var` names a
// variable to look up and only the engine holds the host's variables —
// real Ansible's debug is an action plugin for the same reason.
//
// The result carries _ansible_verbose_always, which is real Ansible's
// own marker telling the stdout callback to print the result in full
// even without -v. It is what makes a debug task show anything at all.
func moduleDebug(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	msg := argString(args, "msg", "Hello world!")
	return Ok(msg).WithExtra(VerboseAlwaysKey, true), nil
}

// VerboseAlwaysKey marks a result a stdout callback should print in full
// without -v. Spelled as real Ansible spells it on the wire.
const VerboseAlwaysKey = "_ansible_verbose_always"

// moduleFail implements Ansible's `fail` module: always fails with msg.
//
// Args: msg (string, default "Failed as requested from task").
func moduleFail(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	msg := argString(args, "msg", "Failed as requested from task")
	return Fail(msg), nil
}

// moduleAssert implements Ansible's `assert` module: fails unless every
// condition in `that` evaluates true. Conditions here are already
// booleans (the caller — the playbook engine — evaluates each Jinja2
// expression in `that` before invoking the module, matching how
// Ansible's own assert action plugin works).
//
// Args: that ([]bool); fail_msg/msg (string); success_msg (string).
func moduleAssert(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	thatRaw, ok := args["that"]
	if !ok {
		return Result{}, errArg("assert: missing required argument: that")
	}
	conditions, ok := thatRaw.([]any)
	if !ok {
		conditions = []any{thatRaw}
	}
	for _, c := range conditions {
		truthy, ok := c.(bool)
		if !ok {
			return Result{}, errArg("assert: condition %v did not evaluate to a boolean (got %T) — evaluate `that` entries before calling this module", c, c)
		}
		if !truthy {
			msg := argString(args, "fail_msg", argString(args, "msg", fmt.Sprintf("Assertion failed: %v", c)))
			return Fail(msg), nil
		}
	}
	msg := argString(args, "success_msg", "All assertions passed")
	return Ok(msg), nil
}
