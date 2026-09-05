package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMemsetDNSReload implements Ansible's `memset_dns_reload` module
// via Memset's own official `ma-shell` — see memset_common.go's own doc
// comment for the CLI-substitution rationale, ma-shell's own invocation
// syntax, and its two verified quirks (no boolean-false, exit 0 on an
// API-level fault) this module relies on below.
//
// Args: api_key (string, required) — see memset_common.go's own doc
// comment for why this is UNAVOIDABLY placed on ma-shell's own argv, a
// genuine deviation from this project's "no secrets in argv" rule; poll
// (bool, default false) — when true, polls `job.status` for up to 30
// seconds (6 attempts, 5s apart), matching real memset_dns_reload.py's
// own `poll_reload_status` bound exactly.
//
// # RPC methods — verified directly in real memset_dns_reload.py's own
// # source
//
// `dns.reload` (no parameters) submits the reload request; `job.status`
// (param: id, the job ID returned by dns.reload) checks its progress.
//
// A poll that never reaches `finished=true` within the bound is NOT a
// task failure — matching real memset_dns_reload.py's own documented
// behavior exactly ("the task does not return as failed, but stderr
// indicates that the polling failed") — Changed is already true because
// the reload request itself was accepted; Extra["stderr"] carries the
// same explanatory text real memset_dns_reload.py's own code emits.
func moduleMemsetDNSReload(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	apiKey, err := requireString(args, "api_key")
	if err != nil {
		return Result{}, errArg("memset_dns_reload: %v", err)
	}
	poll := argBool(args, "poll", false)

	if res, ok := msRequireBinary(ctx, conn, "memset_dns_reload"); !ok {
		return res, nil
	}

	result, problem, err := msCall(ctx, conn, apiKey, "dns.reload", nil)
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_dns_reload: dns.reload: %s", problem)), nil
	}
	job := msObject(result)
	res := Changed("DNS reload requested").WithExtra("memset_api", job)

	if !poll {
		return res, nil
	}

	// Real memset_dns_reload.py's own poll_reload_status ALWAYS makes at
	// least one job.status call once poll=true (regardless of whether
	// dns.reload's own response already reported finished=true) — this
	// port matches that exactly, rather than short-circuiting on the
	// initial job object.
	jobID := fmt.Sprint(job["id"])
	var status map[string]any
	pollOnce := func() (bool, Result, error) {
		sresult, sproblem, serr := msCall(ctx, conn, apiKey, "job.status", []msParam{msStr("id", jobID)})
		if serr != nil {
			return false, Result{}, serr
		}
		if sproblem != "" {
			// A polling probe failure doesn't undo the already-accepted
			// reload request — matching real memset_dns_reload.py's own
			// "don't return this as an overall task failure" stance.
			return false, res.WithExtra("stderr", fmt.Sprintf(
				"Reload submitted successfully, but polling job.status failed: %s", sproblem)), nil
		}
		status = msObject(sresult)
		return true, Result{}, nil
	}

	if ok, early, err := pollOnce(); err != nil {
		return Result{}, err
	} else if !ok {
		return early, nil
	}
	// Real memset_dns_reload.py's own inner loop is bounded to 6
	// attempts (5s apart) per its own docs ("unless the 30 second
	// timeout is reached first") — this port honors that DOCUMENTED
	// bound; the real Python source's outer while loop could in
	// principle re-run the inner 6-attempt loop indefinitely if a job
	// never finishes and never errors, which this port deliberately does
	// NOT replicate (a documented simplification, not an oversight).
	for attempt := 0; attempt < 6 && fmt.Sprint(status["finished"]) != "true"; attempt++ {
		if _, serr := runStatus(ctx, conn, "sleep 5"); serr != nil {
			return Result{}, serr
		}
		if ok, early, err := pollOnce(); err != nil {
			return Result{}, err
		} else if !ok {
			return early, nil
		}
	}

	if fmt.Sprint(status["error"]) == "true" {
		return res.WithExtra("stderr",
			"Reload submitted successfully, but the Memset API returned a job error when attempting to poll the reload status."), nil
	}
	return res.WithExtra("memset_api", status), nil
}
