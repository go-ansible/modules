package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAirbrakeDeployment implements Ansible's `airbrake_deployment`
// (community.general) module: notifies Airbrake of an application
// deployment (https://airbrake.io/docs/api/#deploys-v4) via Airbrake's
// own official CLI, `airbrake` (github.com/airbrake/airbrake-cli,
// installable via `brew install airbrake/airbrake-cli/airbrake`),
// instead of real airbrake_deployment.py's own bare `fetch_url` POST
// to `{url}{project_id}/deploys?key={project_key}` — the same "shell
// out to the platform's own official CLI instead of an API client"
// precedent this port already uses elsewhere.
//
// # A genuine, verified documentation gap in airbrake-cli itself
//
// This is NOT a guess presented as fact: airbrake-cli's own official
// README (fetched directly from GitHub, not a mirror) documents
// `login`, `config set`/`config show`, `install`, `projects list`, and
// `notices create` in full, including every flag each one takes — and
// then, for deploys specifically, says only "For information on all
// the available commands like deploys and more, invoke: `airbrake
// --help`". No other source this port's own research could reach
// (docs.airbrake.io's own CLI page, its own deploy-tracking page, the
// CLI's GitHub Releases changelog, and a further web search) documents
// the deploy command's exact flags either — every hit either repeats
// the same "invoke --help" pointer or only covers the REST API/Rake
// task/Capistrano paths, not the CLI. This port has no `airbrake`
// binary or `--help` output of its own to fall back on.
//
// Rather than fabricate flag names with false confidence, this port's
// own `airbrake deploys create` invocation below is a well-evidenced
// INFERENCE, clearly flagged as such:
//
//   - The subcommand name `deploys create` follows the CLI's own
//     documented `<plural-noun> <verb>` shape used by every other
//     command it DOES document (`projects list`, `notices create`,
//     `sourcemaps create`).
//   - Its per-field flag names (`--project-id`, `--environment`,
//     `--username`, `--repository`, `--revision`, `--version`) match
//     real airbrake_deployment.py's own field names one-for-one, which
//     in turn match Airbrake's own v4 deploys API parameter names
//     (`environment`/`username`/`repository`/`revision`/`version` —
//     confirmed against Airbrake's own v4-vs-earlier migration notes,
//     which document this exact rename from the older
//     `user`/`to`/`revision`/`repo` shape).
//
// If a real `airbrake` binary's own `--help` output ever contradicts
// this, this function's own command line is the place to fix it — this
// comment intentionally does not claim more certainty than the
// research above actually established.
//
// # Auth: no non-argv alternative could be confirmed either — a second,
// # separate honestly-flagged gap
//
// airbrake-cli's own README documents `--project-key` only as a GLOBAL
// flag (alongside `--config`/`--user-key`), with `airbrake login` or
// `airbrake config set user-key ...` as the alternative that persists
// it to `$HOME/.airbrake.yaml` — but every one of the README's own
// worked examples (including `notices create`) passes `--project-id`
// on argv directly and simply omits `--project-key` from the command
// line entirely, implying project-key is meant to already be
// configured via `login`/`config set`, not supplied fresh per
// invocation. This port's own further search for an
// AIRBRAKE_PROJECT_KEY-style environment-variable override (the
// pattern twilio-cli/ovhcloud-cli/xcli all document for their own
// equivalent secrets) found no confirmation in any source this port
// judges reliable enough to act on.
//
// Given that, and per this project's own hard "no secrets in argv"
// rule ("never place a secret/token/password literally on a composed
// command line if the CLI supports any alternative"), this port takes
// the alternative airbrake-cli's own README actually demonstrates:
// project_key is NEVER placed on the `airbrake` command line by this
// port. Instead, project_key is treated the same way this project's
// own "auth precondition convention" already treats a target CLI's own
// login step: `airbrake login` or `airbrake config set user-key
// <project_key>` must already have been run on the target (writing
// `$HOME/.airbrake.yaml`) before this module runs. A caller's own
// project_key argument value is still required (matching real
// airbrake_deployment.py's own required=True) and is used only to
// fail fast with a clear message if empty — this port does not
// silently ignore it, it simply never forwards it to argv, config, or
// environment, since no verified non-argv channel to do so was found.
// project_id (also marked no_log=True in the real module's own
// argument_spec, unusually, alongside project_key) IS placed on argv,
// matching every one of airbrake-cli's own published examples, which
// do exactly that with their own --project-id values.
//
// Args: project_id (required) — sent as `--project-id`; project_key
// (required) — NOT forwarded, see above; environment (required) —
// sent as `--environment`; user (optional, aliases the real module's
// own `user` field onto the CLI's own `--username`, matching Airbrake's
// own v4 rename); repo (optional) — sent as `--repository`; revision
// (optional) — sent as `--revision`; version (optional) — sent as
// `--version`. url and validate_certs (real module's own custom-
// endpoint/Errbit-compatibility knobs) have no equivalent in
// airbrake-cli, which always targets Airbrake's own SaaS API; giving
// a non-default url therefore fails cleanly rather than being silently
// ignored.
//
// Deviation — non-idempotent, matching real airbrake_deployment.py's
// own behavior exactly (it has no read-before-write check of any
// kind): this port always reports Changed=true on a zero exit.
func moduleAirbrakeDeployment(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "airbrake_deployment"
	projectID, err := requireString(args, "project_id")
	if err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "project_key"); err != nil {
		return Result{}, err
	}
	environment, err := requireString(args, "environment")
	if err != nil {
		return Result{}, err
	}
	if u := argString(args, "url", "https://api.airbrake.io/api/v4/projects/"); u != "https://api.airbrake.io/api/v4/projects/" {
		return Fail(mod + ": a custom `url` (e.g. for an Errbit-compatible endpoint) is not supported by this " +
			"port — airbrake-cli always targets Airbrake's own SaaS API and has no per-invocation endpoint " +
			"override; see moduleAirbrakeDeployment's own doc comment"), nil
	}

	if res, ok := airbrakeRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}

	argv := []string{"deploys", "create", "--project-id", projectID, "--environment", environment}
	if user := argString(args, "user", ""); user != "" {
		argv = append(argv, "--username", user)
	}
	if repo := argString(args, "repo", ""); repo != "" {
		argv = append(argv, "--repository", repo)
	}
	if revision := argString(args, "revision", ""); revision != "" {
		argv = append(argv, "--revision", revision)
	}
	if version := argString(args, "version", ""); version != "" {
		argv = append(argv, "--version", version)
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	res, err := conn.Exec(ctx, "airbrake "+strings.Join(quoted, " "), nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(mod + ": airbrake deploys create failed: " + airbrakeErrMsg(res)), nil
	}
	return Changed(""), nil
}

func airbrakeRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v airbrake"); err != nil {
		return Fail(fmt.Sprintf("%s: the airbrake binary (Airbrake's own official CLI, airbrake-cli) is required "+
			"on the target and was not found in PATH — this port shells out to it rather than POSTing to "+
			"Airbrake's v4 deploys API directly; see airbrake_deployment.go's own doc comment, including the "+
			"precondition that `airbrake login` or `airbrake config set user-key <project_key>` must already "+
			"have been run on the target", moduleName)), false
	}
	return Result{}, true
}

func airbrakeErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
