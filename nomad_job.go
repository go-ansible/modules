package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// nomadConnArgs builds the `nomad` CLI's own global HTTP-client flags
// (-address/-ca-cert/-client-cert/-client-key/-tls-skip-verify/
// -namespace) shared by every nomad_* module in this batch, matching
// the host/port/use_ssl/validate_certs/client_cert/client_key/
// namespace options every real nomad_* (community.general) module
// documents identically. Real nomad_job/nomad_job_info/nomad_token are
// all implemented against python-nomad's own HTTP API client; this
// port has no Go Nomad client wired into remoteexec.Connection, so it
// substitutes shelling out to the `nomad` CLI instead — the same
// substitution this project already makes for consul_kv.go (`consul`
// CLI) and lxd_container.go (`lxc` CLI).
func nomadConnArgs(args map[string]any) []string {
	scheme := "https"
	if !argBool(args, "use_ssl", true) {
		scheme = "http"
	}
	host, _ := requireString(args, "host")
	port := argInt(args, "port", 4646)
	a := []string{"-address=" + scheme + "://" + host + ":" + strconv.Itoa(port)}
	if !argBool(args, "validate_certs", true) {
		a = append(a, "-tls-skip-verify")
	}
	if cc := argString(args, "client_cert", ""); cc != "" {
		a = append(a, "-client-cert="+cc)
	}
	if ck := argString(args, "client_key", ""); ck != "" {
		a = append(a, "-client-key="+ck)
	}
	if ns := argString(args, "namespace", ""); ns != "" {
		a = append(a, "-namespace="+ns)
	}
	return a
}

// nomadRun runs `nomad <action> <connArgs> <opts...>` on the target,
// with stdin passed through unquoted (jobspec content), and the
// `token` argument (if set) via the NOMAD_TOKEN environment variable
// rather than a CLI flag — keeping it out of the target's process
// listing, matching consul_kv.go's own consulKv and redis.go's own
// redisCli conventions.
func nomadRun(ctx context.Context, conn remoteexec.Connection, args map[string]any, argv []string, stdin string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := strings.Join(quoted, " ")
	if tok := argString(args, "token", ""); tok != "" {
		cmd = "NOMAD_TOKEN=" + shellQuote(tok) + " " + cmd
	}
	if stdin != "" {
		return conn.Exec(ctx, cmd, strings.NewReader(stdin))
	}
	return conn.Exec(ctx, cmd, nil)
}

// moduleNomadJob implements Ansible's `nomad_job` (community.general)
// module: submits or stops a HashiCorp Nomad job via the `nomad` CLI —
// see nomadConnArgs's own doc comment for why this port substitutes
// the CLI for real nomad_job's python-nomad HTTP API client.
//
// Args: host (required); port (default 4646); use_ssl (default true);
// validate_certs (default true); client_cert/client_key; namespace;
// token (via NOMAD_TOKEN, see nomadRun); timeout (default 5, accepted
// for compatibility but not wired to a per-command timeout — this
// port's Connection.Exec has no per-call deadline knob independent of
// ctx, so a caller wanting a hard timeout should set it on ctx
// itself); state (present|absent, required); name — job name, for
// state=absent, or state=present with force_start and no content;
// content — the jobspec text, for state=present; content_format
// (hcl|json, default "hcl"); force_start (bool) — see below.
//
// state=present with content: the content is piped to `nomad job run
// -detach -` on stdin (`-json` added when content_format=json,
// matching real nomad's own `-json` flag for a complete Job-struct
// JSON document rather than HCL); this port has no cheap way to
// compare the submitted jobspec against Nomad's own normalized,
// server-side-expanded stored version through the CLI's text output
// (real nomad_job's own python-nomad client can diff structured Job
// objects; a CLI substitution cannot without reimplementing Nomad's
// own HCL2 parser), so — matching this project's documented "can't
// cheaply tell apart" convention (see pacman.go's own state=latest) —
// this path always reports Changed=true.
//
// state=present with name only (no content) and force_start=true:
// fetches the job's own last-submitted spec via `nomad job inspect
// <name> -json`, clears its "Stop" field, and resubmits it via `nomad
// job run -json -detach -` — the closest CLI-only equivalent to real
// nomad_job's own force_start (python-nomad re-registers the existing
// job definition with Stop cleared); Changed=true only if the job was
// actually stopped beforehand (Stop==true) or absent (inspect fails,
// treated as nothing to force-start, surfaced as Result{Failed:true}
// rather than silently no-op-ing, since force_start with no content
// and no existing job has nothing to resubmit).
//
// state=present with name only and force_start=false: read-only,
// unchanged if the job exists at all (regardless of its own Stop/
// status), matching real nomad_job's own "if not force_start, do
// nothing but confirm existence" branch; Fail if it does not exist.
//
// state=absent: `nomad job stop <name>`, Changed=true unless the job
// was already stopped/nonexistent (checked first via `nomad job status
// <name> -json`'s own "Stop" field; a job not found at all is treated
// as already absent).
func moduleNomadJob(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := requireString(args, "host"); err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("nomad_job: state must be present or absent, got %q", state)
	}
	content := argString(args, "content", "")
	name := argString(args, "name", "")
	if content == "" && name == "" {
		return Result{}, errArg("nomad_job: one of content or name is required")
	}

	if state == "absent" {
		if name == "" {
			return Result{}, errArg("nomad_job: name is required when state=absent")
		}
		job, found, err := nomadJobInspect(ctx, conn, args, name)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Ok(name + " already absent"), nil
		}
		if stop, _ := job["Stop"].(bool); stop {
			return Ok(name + " already stopped"), nil
		}
		argv := append([]string{"nomad", "job", "stop", name}, nomadConnArgs(args)...)
		res, err := nomadRun(ctx, conn, args, argv, "")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("nomad_job: stopping " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(name + " stopped"), nil
	}

	if content != "" {
		argv := []string{"nomad", "job", "run", "-detach"}
		if argString(args, "content_format", "hcl") == "json" {
			argv = append(argv, "-json")
		}
		argv = append(argv, nomadConnArgs(args)...)
		argv = append(argv, "-")
		res, err := nomadRun(ctx, conn, args, argv, content)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("nomad_job: running job: "+strings.TrimSpace(res.Stderr)).WithExtra("stderr", res.Stderr), nil
		}
		return Changed("job submitted").WithExtra("stdout", res.Stdout), nil
	}

	// name only.
	job, found, err := nomadJobInspect(ctx, conn, args, name)
	if !argBool(args, "force_start", false) {
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail(name + " not found"), nil
		}
		return Ok(name + " already present"), nil
	}
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Fail("nomad_job: force_start requested for " + name + " but no existing job definition was found to resubmit"), nil
	}
	stopped, _ := job["Stop"].(bool)
	if !stopped {
		return Ok(name + " already running"), nil
	}
	job["Stop"] = false
	spec, err := json.Marshal(map[string]any{"Job": job})
	if err != nil {
		return Result{}, fmt.Errorf("nomad_job: encoding job spec for %s: %w", name, err)
	}
	argv := append([]string{"nomad", "job", "run", "-json", "-detach"}, nomadConnArgs(args)...)
	argv = append(argv, "-")
	res, err := nomadRun(ctx, conn, args, argv, string(spec))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("nomad_job: force-starting " + name + ": " + strings.TrimSpace(res.Stderr)), nil
	}
	return Changed(name + " force started"), nil
}

// nomadJobInspect runs `nomad job inspect <name> -json` and decodes
// its own top-level "Job" object; found is false (not an error) for a
// job that doesn't exist.
func nomadJobInspect(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string) (map[string]any, bool, error) {
	argv := append([]string{"nomad", "job", "inspect", name, "-json"}, nomadConnArgs(args)...)
	res, err := nomadRun(ctx, conn, args, argv, "")
	if err != nil {
		return nil, false, err
	}
	if res.RC != 0 {
		return nil, false, nil
	}
	var wrapper struct {
		Job map[string]any `json:"Job"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &wrapper); err != nil {
		return nil, false, fmt.Errorf("nomad_job: parsing nomad job inspect output for %s: %w", name, err)
	}
	return wrapper.Job, true, nil
}
