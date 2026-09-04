package modules

import (
	"context"
	"sort"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRundeckJobRun implements Ansible's `rundeck_job_run` module:
// runs a Rundeck job by ID and, by default, waits for it to finish, via
// the `rd` CLI (see rundeck_common.go's own doc comment).
//
// Args: job_id (required); job_options (dict, string values only —
// see below); filter_nodes (string, node-filter syntax, passed as
// `--filter`); loglevel (debug|verbose|info|warn|error, default
// "info" — uppercased for `--loglevel`, matching real
// rundeck_job_run's own `self.loglevel = ...upper()`); wait_execution
// (bool, default true); wait_execution_delay (int seconds, default 5);
// wait_execution_timeout (int seconds, default 120); abort_on_timeout
// (bool, default false); url (required); api_token (required).
//
// A job_options value that is not a string is NOT treated as a module
// failure — matching real rundeck_job_run's own verified source
// exactly (`self.module.exit_json(msg=..., execution_info={})`, an
// exit_json call, not fail_json — Changed/Failed both stay false),
// this port returns Ok() with that same message and an empty
// execution_info, faithfully reproducing what reads as a real bug (a
// non-string option silently "succeeds" with an empty result instead
// of failing) rather than silently improving on it — see
// pacemaker_cluster.go's own `force` dead-argument note and
// stacki_host.go's own changed=false note for this project's identical
// stance elsewhere.
//
// run_at_time (an absolute ISO-8601 schedule time, real
// rundeck_job_run's own `runAtTime` REST field) has NO `rd run`
// equivalent this port could verify: rundeck-cli's own documented
// scheduling flag is `--delay` (a RELATIVE duration from now, e.g.
// "48h"), not an absolute-datetime flag — a genuinely different
// primitive, not a renaming this port could translate. Rather than
// silently reinterpreting an absolute schedule time as some relative
// delay (which could schedule a job at a wildly wrong time), this port
// fails this argument cleanly: Result{Failed:true}, not a Go error,
// since the request is well-formed, just not something this port's
// architecture can satisfy — same stance as one_vm.go's own documented
// exact_count/count_attributes gap.
//
// Triggering the run: `rd run --id <job_id> [--filter <filter_nodes>]
// --loglevel <LEVEL> [-- -optname "value" ...]`, RD_FORMAT=json (see
// rundeck_common.go). This port decodes that invocation's own stdout
// directly as the execution's JSON representation (the same shape
// real rundeck_job_run's own `job/{id}/run` REST call returns) — an
// assumption this port could not verify against a live `rd` binary
// (see rundeck_common.go's own doc comment on why), reasonable because
// rundeck-cli's own RD_FORMAT=json mode is documented to serialize
// each subcommand's underlying REST response object, but genuinely
// unverified; if that assumption is wrong for a given rd version, this
// module fails cleanly on the JSON-decode error rather than silently
// misreporting a bogus id.
//
// wait_execution=true (the default): this port polls `rd executions
// info -e <execution id>`, RD_FORMAT=json, every wait_execution_delay
// seconds until status is one of succeeded/failed/aborted/scheduled or
// wait_execution_timeout elapses — a direct, testable port of real
// rundeck_job_run's own job_status_check poll loop (not a documented
// gap the way pacemaker_resource.go's own `wait` is: this one has a
// real, deterministic HTTP-equivalent poll to drive, so this port
// implements it exactly, sleeping via time.Sleep like the real module
// sleeps via Python's time.sleep). On timeout, if abort_on_timeout,
// runs `rd executions kill -e <id>` (rundeck-cli's own documented
// `kill` subcommand — the CLI's name for real rundeck_job_run's own
// `execution/{id}/abort` REST call) then re-polls once more for the
// abort's own outcome before failing; otherwise fails immediately with
// the last-seen execution_info, matching real rundeck_job_run's own
// dispatch exactly.
//
// Extra field "execution_info" mirrors real rundeck_job_run's own
// identically-named return value.
func moduleRundeckJobRun(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	jobID, err := requireString(args, "job_id")
	if err != nil {
		return Result{}, err
	}
	loglevel := argString(args, "loglevel", "info")
	switch loglevel {
	case "debug", "verbose", "info", "warn", "error":
	default:
		return Result{}, errArg("rundeck_job_run: loglevel must be one of debug, verbose, info, warn, error, got %q", loglevel)
	}
	if argString(args, "run_at_time", "") != "" {
		return Fail("rundeck_job_run: run_at_time (an absolute schedule time) has no `rd run` equivalent this port " +
			"could verify — rundeck-cli's own documented scheduling flag (--delay) is relative, not absolute; " +
			"see this module's own doc comment"), nil
	}
	jobOptions, msg, ok := rundeckJobOptionStrings(args["job_options"])
	if !ok {
		return Ok(msg).WithExtra("execution_info", map[string]any{}), nil
	}
	filterNodes := argString(args, "filter_nodes", "")
	waitExecution := argBool(args, "wait_execution", true)
	waitDelay := argInt(args, "wait_execution_delay", 5)
	waitTimeout := argInt(args, "wait_execution_timeout", 120)
	abortOnTimeout := argBool(args, "abort_on_timeout", false)

	url, token, err := rdAuth(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := rdRequireBinary(ctx, conn, "rundeck_job_run"); !ok {
		return res, nil
	}

	argv := []string{"run", "--id", jobID, "--loglevel", strings.ToUpper(loglevel)}
	if filterNodes != "" {
		argv = append(argv, "--filter", filterNodes)
	}
	if len(jobOptions) > 0 {
		argv = append(argv, "--")
		keys := make([]string, 0, len(jobOptions))
		for k := range jobOptions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			argv = append(argv, "-"+k, jobOptions[k])
		}
	}

	var execInfo map[string]any
	res, err := rdRunJSON(ctx, conn, url, token, &execInfo, argv...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("rundeck_job_run: " + rdErrMsg(res)), nil
	}
	execID, _ := execInfo["id"]
	if !waitExecution {
		out := Ok("Job run send successfully!")
		return out.WithExtra("execution_info", execInfo), nil
	}

	final, timedOut, err := rundeckJobPoll(ctx, conn, url, token, execID, waitTimeout, waitDelay)
	if err != nil {
		return Result{}, err
	}
	status, _ := final["status"].(string)
	if timedOut {
		if abortOnTimeout {
			if _, err := rdRun(ctx, conn, url, token, "executions", "kill", "-e", fmtAny(execID)); err != nil {
				return Result{}, err
			}
			aborted, _, err := rundeckJobPoll(ctx, conn, url, token, execID, waitTimeout, waitDelay)
			if err != nil {
				return Result{}, err
			}
			return Fail("Job execution aborted due the timeout specified").WithExtra("execution_info", aborted), nil
		}
		return Fail("Job execution timed out").WithExtra("execution_info", final), nil
	}
	switch status {
	case "failed":
		return Fail("Job execution failed").WithExtra("execution_info", final), nil
	case "scheduled":
		out := Changed("Job scheduled to run")
		return out.WithExtra("execution_info", final), nil
	default: // succeeded, aborted, or an unrecognized terminal status
		out := Ok("Job execution succeeded!")
		return out.WithExtra("execution_info", final), nil
	}
}

// rundeckJobOptionStrings validates that every job_options value is a
// string, matching real rundeck_job_run's own per-key check. ok=false
// means "return Ok(msg) immediately", faithfully reproducing real
// rundeck_job_run's own exit_json (not fail_json) here — see this
// file's own doc comment.
func rundeckJobOptionStrings(v any) (opts map[string]string, msg string, ok bool) {
	m, mok := v.(map[string]any)
	if !mok {
		return nil, "", true
	}
	opts = make(map[string]string, len(m))
	for k, val := range m {
		s, sok := val.(string)
		if !sok {
			return nil, "Job option '" + k + "' value must be a string", false
		}
		opts[k] = s
	}
	return opts, "", true
}

// rundeckJobPoll polls `rd executions info -e <id>` every delaySec
// seconds until its status is a terminal one (succeeded/failed/
// aborted/scheduled) or timeoutSec elapses.
func rundeckJobPoll(ctx context.Context, conn remoteexec.Connection, url, token string, execID any, timeoutSec, delaySec int) (map[string]any, bool, error) {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for {
		var info map[string]any
		res, err := rdRunJSON(ctx, conn, url, token, &info, "executions", "info", "-e", fmtAny(execID))
		if err != nil {
			return nil, false, err
		}
		if res.RC == 0 {
			switch info["status"] {
			case "aborted", "scheduled", "failed", "succeeded":
				return info, false, nil
			}
		}
		if !time.Now().Before(deadline) {
			return info, true, nil
		}
		time.Sleep(time.Duration(delaySec) * time.Second)
	}
}
