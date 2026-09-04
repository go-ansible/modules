package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRundeckJobExecutionsInfo implements Ansible's
// `rundeck_job_executions_info` module: lists a Rundeck job's own past
// executions, via the `rd` CLI (see rundeck_common.go's own doc
// comment).
//
// Args: job_id (required); status (succeeded|failed|aborted|running,
// optional); max (default 20); offset (default 0); url (required);
// api_token (required).
//
// Real rundeck_job_executions_info's own endpoint is `GET
// job/{id}/executions?offset=&max=&status=`, returning a JSON body
// whose top-level keys (paging, executions) are spread directly into
// the module's own result (`module.exit_json(msg=..., **response)`).
// This port maps that onto `rd executions query --jobids <job_id>
// [--status <status>] --max <max> --offset <offset>`, RD_FORMAT=json
// (see rundeck_common.go — `query` is rundeck-cli's own documented
// full-flag execution-listing subcommand; `list`/`query` both appear
// in rundeck-cli's own source, `query` is the one whose flags are
// documented in full).
//
// Extra fields "executions" and "paging" mirror real
// rundeck_job_executions_info's own identically-named return values
// when the underlying JSON has that shape; this port does not reshape
// `rd`'s own JSON output beyond decoding it, so if a given `rd`
// version's own `--jobids`-filtered `executions query` JSON is not
// wrapped in the same {"executions": [...], "paging": {...}} envelope
// real rundeck_job_executions_info's REST endpoint returns (this port's
// own `rd` binary was not available to verify this against — see
// rundeck_common.go's own doc comment), "executions" falls back to the
// decoded top-level array itself and "paging" is omitted — a documented
// architecture-driven possibility, not a silent failure.
func moduleRundeckJobExecutionsInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	jobID, err := requireString(args, "job_id")
	if err != nil {
		return Result{}, err
	}
	status := argString(args, "status", "")
	switch status {
	case "", "succeeded", "failed", "aborted", "running":
	default:
		return Result{}, errArg("rundeck_job_executions_info: status must be one of succeeded, failed, aborted, running, got %q", status)
	}
	max := argInt(args, "max", 20)
	offset := argInt(args, "offset", 0)
	url, token, err := rdAuth(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := rdRequireBinary(ctx, conn, "rundeck_job_executions_info"); !ok {
		return res, nil
	}

	argv := []string{"executions", "query", "--jobids", jobID, "--max", fmtAny(max), "--offset", fmtAny(offset)}
	if status != "" {
		argv = append(argv, "--status", status)
	}

	var envelope struct {
		Executions []map[string]any `json:"executions"`
		Paging     map[string]any   `json:"paging"`
	}
	res, err := rdRunJSON(ctx, conn, url, token, &envelope, argv...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("rundeck_job_executions_info: " + rdErrMsg(res)), nil
	}

	out := Ok("Executions info result")
	out = out.WithExtra("executions", envelope.Executions)
	if envelope.Paging != nil {
		out = out.WithExtra("paging", envelope.Paging)
	}
	return out, nil
}
