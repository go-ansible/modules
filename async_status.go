package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAsyncStatus implements Ansible's `async_status` module: checks
// on a previously started asynchronous task by its job ID.
//
// A task launched with `async:` (only command/shell — see
// playbook.Engine's own async handling and AsyncLaunch's doc comment
// for why this port can't genuinely background arbitrary modules the
// way real Ansible does) is backgrounded on the target via AsyncLaunch,
// under ~/.ansible_async/<jid>. This module polls that same job
// through AsyncCheck/AsyncCleanup — real, working status/cleanup now,
// not the permanent hard-fail this module used to be before
// go-ansible/playbook implemented the launching side of async: at all.
//
// Args: jid (string, required); mode (status|cleanup, default
// "status").
func moduleAsyncStatus(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	jid, err := requireString(args, "jid")
	if err != nil {
		return Result{}, err
	}
	mode := argString(args, "mode", "status")
	if mode != "status" && mode != "cleanup" {
		return Result{}, errArg("async_status: mode must be status or cleanup, got %q", mode)
	}

	if mode == "cleanup" {
		if err := AsyncCleanup(ctx, conn, jid); err != nil {
			return Result{}, err
		}
		return Ok("job cleaned up").
			WithExtra("ansible_job_id", jid).
			WithExtra("erased", jid), nil
	}

	found, done, rc, stdout, stderr, err := AsyncCheck(ctx, conn, jid)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Fail("could not find job").
			WithExtra("ansible_job_id", jid).
			WithExtra("started", true).
			WithExtra("finished", true), nil
	}
	r := Ok("").
		WithExtra("ansible_job_id", jid).
		WithExtra("started", true).
		WithExtra("finished", done)
	if !done {
		return r, nil
	}
	r.Failed = rc != 0
	r = r.WithExtra("rc", rc).WithExtra("stdout", stdout).WithExtra("stderr", stderr)
	return r, nil
}
