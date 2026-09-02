package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAsyncStatus implements Ansible's `async_status` module: checks
// on a previously started asynchronous task by its job ID.
//
// Real ansible.builtin.async_status only works together with the
// `async`/`poll` task-level mechanism: a task launched with `async:` is
// backgrounded on the target with its own results file under
// `~/.ansible_async/`, and async_status polls that file for `jid`. This
// port implements no such mechanism at all — `async`/`poll` is an
// engine-level feature (how a task is launched and subsequently
// awaited), out of scope for a single module, and go-ansible's engine
// never backgrounds a task that way. Rather than silently returning a
// fabricated "finished" result (which would misrepresent an
// unimplemented feature as a working one), async_status always fails
// with a clear, on-topic message — this package's convention of
// failing loud instead of being silently wrong.
//
// Args: jid (string, required); mode (status|cleanup, default
// "status") — accepted and validated so a real playbook's async_status
// task gets a specific failure naming the actual gap, rather than a
// generic argument error; both modes fail identically here, since
// neither has anything to check or clean up.
func moduleAsyncStatus(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	jid, err := requireString(args, "jid")
	if err != nil {
		return Result{}, err
	}
	mode := argString(args, "mode", "status")
	if mode != "status" && mode != "cleanup" {
		return Result{}, errArg("async_status: mode must be status or cleanup, got %q", mode)
	}
	return Fail("async_status: async task execution is not implemented in this port; " +
		"there is no backgrounded job " + jid + " to check or clean up"), nil
}
