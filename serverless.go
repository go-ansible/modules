package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleServerless implements Ansible's `serverless`
// (community.general) module: wraps the Serverless Framework CLI
// (`serverless deploy`/`serverless remove`) for a project directory —
// the same "wrap a local CLI whose own providers may talk to a cloud,
// but the module itself is a local process invocation" shape this
// port's own terraform.go already documents (there is no library form
// to substitute here either: real serverless.py already just shells
// out to the `serverless` binary itself, exactly like real terraform
// does).
//
// Args: service_path (required) — cwd for the `serverless` invocation,
// and the directory this port reads `serverless.yml` from (see below);
// state (present|absent, default present) — `deploy`/`remove`;
// serverless_bin_path (optional) — an explicit binary, else plain
// `serverless` (assumed on PATH; unlike some other CLI-wrapping
// modules in this port, no `command -v` pre-check is done, matching
// real serverless.py's own `module.get_bin_path('serverless')`, which
// DOES fail cleanly if missing — this port instead lets the shell's own
// "command not found" surface as a non-zero exit, observably the same
// outcome); region (string, default ""); stage (string, default "");
// deploy (bool, default true) — `--noDeploy` when false; force (bool,
// default false) — `--force`, only applied when deploy=true (state=
// present), matching real serverless.py's own `elif force:` nested
// under `if not deploy: ... elif force: ...`; verbose (bool, default
// false) — `--verbose`.
//
// Deviation — reading serverless.yml: real serverless.py reads
// `<service_path>/serverless.yml` with Python's `yaml.safe_load` (a
// full YAML parser) to recover its own top-level `service`/`stage`
// keys for Extra["service_name"]. This port has no YAML library
// anywhere in its dependency graph (gopkg.in/yaml.v3 appears only as
// an INDIRECT transitive dependency of another module, never
// imported directly by this package) and does not add one for this
// single narrow need; instead it implements a minimal top-level-key
// scanner (serverlessConfigKey) that recognizes only plain `service:
// value` / `stage: value` lines at column zero — sufficient for
// every real serverless.yml this port has seen documented (a flat
// top-level scalar for each), but it will NOT correctly resolve a
// `service`/`stage` key using YAML's flow-mapping, multi-document, or
// anchor/alias syntax, which a full parser would. A documented,
// narrow gap, not a silent one.
//
// Command construction mirrors real serverless.py's own main() flow
// (including string ordering) exactly; the run itself happens with
// `service_path` as the target Connection's own working directory —
// composed as `cd <service_path> && <command>`, the same convention
// command.go's own moduleCommand chdir handling uses.
//
// Deviation — a genuine, VERIFIED real-module quirk preserved
// faithfully: on a SUCCESSFUL run (rc==0), real serverless.py's own
// closing `module.exit_json(changed=True, state="present", ...)` call
// passes the LITERAL STRING "present" for Extra["state"] regardless of
// whether state was actually "present" or "absent" — i.e. even a
// successful `serverless remove` (state=absent) reports
// Extra["state"]=="present", asymmetric with the explicit
// state="absent" its own special not-found failure branch (see below)
// sets. This is real, verified upstream behavior (read from
// serverless.py's own source before porting, per this project's hard
// bibliography-before rule), not a bug introduced here — this port
// reproduces it exactly rather than "fixing" it to the more sensible
// `state` value.
//
// On a non-zero exit: if state=absent AND stdout contains the literal
// substring `-<stage>' does not exist` (real serverless.py's own exact
// check, including the leading hyphen — this is `remove`'s own message
// for "no such deployed stack"), this port returns Ok (Changed=false,
// Extra["state"]="absent" — this specific branch DOES use the real
// `state` value, matching real serverless.py's own explicit
// `state="absent"` argument there) with Extra["service_name"] still
// populated; any other non-zero exit is a Fail with combined stdout/
// stderr.
func moduleServerless(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	servicePath, err := requireString(args, "service_path")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("serverless: state must be one of present, absent, got %q", state)
	}
	region := argString(args, "region", "")
	stage := argString(args, "stage", "")
	deploy := argBool(args, "deploy", true)
	force := argBool(args, "force", false)
	verbose := argBool(args, "verbose", false)
	bin := argString(args, "serverless_bin_path", "serverless")

	argv := []string{bin}
	if state == "present" {
		argv = append(argv, "deploy")
		if !deploy {
			argv = append(argv, "--noDeploy")
		} else if force {
			argv = append(argv, "--force")
		}
	} else {
		argv = append(argv, "remove")
	}
	if region != "" {
		argv = append(argv, "--region", region)
	}
	if stage != "" {
		argv = append(argv, "--stage", stage)
	}
	if verbose {
		argv = append(argv, "--verbose")
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	command := strings.Join(quoted, " ")
	full := "cd " + shellQuote(servicePath) + " && " + command

	res, err := conn.Exec(ctx, full, nil)
	if err != nil {
		return Result{}, err
	}

	if res.RC != 0 {
		if state == "absent" && strings.Contains(res.Stdout, "-"+stage+"' does not exist") {
			serviceName, serr := serverlessServiceName(ctx, conn, servicePath, stage)
			if serr != nil {
				return Result{}, serr
			}
			r := Ok("")
			r = r.WithExtra("state", "absent").WithExtra("command", command).
				WithExtra("out", res.Stdout).WithExtra("service_name", serviceName)
			return r, nil
		}
		return Fail("serverless: Failure when executing Serverless command. Exited " + strconv.Itoa(res.RC) +
			".\nstdout: " + res.Stdout + "\nstderr: " + res.Stderr), nil
	}

	serviceName, err := serverlessServiceName(ctx, conn, servicePath, stage)
	if err != nil {
		return Result{}, err
	}
	// See this function's own doc comment: real serverless.py hardcodes
	// the literal string "present" here regardless of the actual state.
	r := Changed("")
	r = r.WithExtra("state", "present").WithExtra("out", res.Stdout).
		WithExtra("command", command).WithExtra("service_name", serviceName)
	return r, nil
}

// serverlessServiceName reads <servicePath>/serverless.yml (see
// moduleServerless's own doc comment on this port's minimal top-level
// scanner in place of a full YAML parser) and returns
// "<service>-<stage>" if stage is given, else
// "<service>-<config's own stage, defaulting to dev>", matching real
// serverless.py's own get_service_name() exactly. Fails
// (Result{Failed:true}) if the file can't be read or has no `service`
// key, matching real serverless.py's own module.fail_json calls there
// — but since this is called from a context that must return a plain
// error to unwind cleanly, the caller re-wraps failures itself; this
// helper instead returns a Go error, treated as an infra/well-formed-
// request-can't-proceed condition consistent with this port's own
// house convention for a helper several call sites share.
func serverlessServiceName(ctx context.Context, conn remoteexec.Connection, servicePath, stage string) (string, error) {
	path := servicePath + "/serverless.yml"
	res, err := runStatus(ctx, conn, "cat "+shellQuote(path))
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", errArg("serverless: could not open serverless.yml in %s", path)
	}
	service, cfgStage := serverlessConfigKeys(res.Stdout)
	if service == "" {
		return "", errArg("serverless: could not read `service` key from serverless.yml file")
	}
	if stage != "" {
		return service + "-" + stage, nil
	}
	if cfgStage == "" {
		cfgStage = "dev"
	}
	return service + "-" + cfgStage, nil
}

// serverlessConfigKeys scans yml's own top-level (column-zero) lines
// for `service:`/`stage:` keys — see moduleServerless's own doc
// comment for why this is a minimal scanner, not a full YAML parser.
func serverlessConfigKeys(yml string) (service, stage string) {
	for _, line := range strings.Split(yml, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "service":
			service = val
		case "stage":
			stage = val
		}
	}
	return service, stage
}
