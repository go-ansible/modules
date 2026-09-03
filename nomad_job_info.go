package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleNomadJobInfo implements Ansible's `nomad_job_info`
// (community.general) module: read-only fetch of one Nomad job's
// status, or a listing of every job, via the `nomad` CLI — see
// nomad_job.go's own nomadConnArgs doc comment for why this port
// substitutes the CLI for real nomad_job_info's python-nomad HTTP API
// client.
//
// Args: host (required); port (default 4646); use_ssl/validate_certs
// (default true); client_cert/client_key; namespace; token (via
// NOMAD_TOKEN); timeout (default 5, accepted, see nomad_job.go's own
// note on why it isn't wired to a per-command deadline); name — if
// set, `nomad job status <name> -json` (a single job's full status
// dict, matching real nomad_job_info's own `get info for one job`
// mode); if unset, `nomad job status -json` (a listing across every
// job, matching real nomad_job_info's own `list Nomad jobs` mode).
//
// Extra["result"]: a []any — the decoded `nomad job status -json`
// output for a single name (wrapped in a one-element list, matching
// real nomad_job_info's own documented `result` return shape being a
// list either way), or the full list for an all-jobs query.
//
// Deviation from real nomad_job_info: real nomad_job_info's own
// per-job dict (via python-nomad's `get_job`) is Nomad's full Job
// object (AllAtOnce, Datacenters, TaskGroups, ...); `nomad job status
// -json` for a single job returns that same full Job object, so a
// name-scoped query matches real nomad_job_info closely. The all-jobs
// listing mode is different: `nomad job status -json` with no job ID
// returns Nomad's own job-summary list (ID/Name/Status/... — fewer
// fields than a single full Job object), while real nomad_job_info's
// own all-jobs mode iterates and calls `get_job` per job, returning
// full Job objects for every one. This port does not do that N+1
// expansion (to avoid one `nomad` invocation per job on a possibly
// large cluster) — Extra["result"] for an all-jobs query is Nomad's
// own summary shape, not the full per-job shape a name-scoped query
// or real nomad_job_info's own all-jobs mode returns.
//
// Never Changed — this module only ever reads.
func moduleNomadJobInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := requireString(args, "host"); err != nil {
		return Result{}, err
	}
	name := argString(args, "name", "")

	argv := []string{"nomad", "job", "status"}
	if name != "" {
		argv = append(argv, name)
	}
	argv = append(argv, "-json")
	argv = append(argv, nomadConnArgs(args)...)
	res, err := nomadRun(ctx, conn, args, argv, "")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		if name != "" {
			return Fail("nomad_job_info: " + name + " not found: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Fail("nomad_job_info: listing jobs: " + strings.TrimSpace(res.Stderr)), nil
	}

	if name != "" {
		var job map[string]any
		if err := json.Unmarshal([]byte(res.Stdout), &job); err != nil {
			return Result{}, fmt.Errorf("nomad_job_info: parsing nomad job status output: %w", err)
		}
		return Ok("").WithExtra("result", []any{job}), nil
	}

	var jobs []any
	if err := json.Unmarshal([]byte(res.Stdout), &jobs); err != nil {
		return Result{}, fmt.Errorf("nomad_job_info: parsing nomad job status output: %w", err)
	}
	return Ok("").WithExtra("result", jobs), nil
}
