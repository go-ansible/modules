package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRollbarDeployment implements Ansible's `rollbar_deployment`
// module via Rollbar's own official `rollbar-cli` (github.com/rollbar/
// rollbar-cli, npm package `rollbar-cli`, installed globally via `npm
// install -g rollbar-cli` and invoked directly as `rollbar-cli` — its
// own package.json `bin` entry maps the command name `rollbar-cli` to
// `bin/rollbar`, verified directly against the published package.json,
// not npx) instead of real rollbar_deployment.py's own hand-rolled
// `fetch_url` POST to https://api.rollbar.com/api/1/deploy/ — the same
// "shell out to the platform's own official CLI instead of an API
// client" precedent this port already uses elsewhere (see
// github_common.go's own doc comment for the fuller rationale).
//
// # Command mapping — verified directly against rollbar-cli's own
// # source (src/deploy/command.js on the master branch), not guessed
//
// `rollbar-cli notify-deploy` posts the exact same fields real
// rollbar_deployment.py sends (access_token, environment, revision,
// local_username, rollbar_username, comment) to Rollbar's own deploy
// API. Its flags (yargs `.option(...)` calls in command.js) map
// one-for-one onto real rollbar_deployment.py's own arguments:
//
//	real arg      -> rollbar-cli flag         (both demandOption:true —
//	                                            i.e. required by the CLI
//	                                            itself, matching real
//	                                            module's own required=true)
//	token          -> --access-token           (required)
//	environment    -> --environment            (required)
//	revision       -> --code-version           (required)
//	user           -> --local-username
//	rollbar_user   -> --rollbar-username
//	comment        -> --comment
//
// rollbar-cli's own --deploy-id and --status flags have no corresponding
// real rollbar_deployment.py argument at all (real module always posts a
// brand-new deploy with no status, matching rollbar-cli's own documented
// "succeeded" default when --status is omitted) — this port never passes
// either.
//
// # Auth: --access-token is UNAVOIDABLY on argv — a verified, real gap
// # in this project's own "no secrets in argv" rule, not an oversight
//
// Unlike `gh`/`ovhcloud`/`hb` (all of which accept a credential via an
// environment variable this port can lean on instead), rollbar-cli's own
// src/deploy/command.js defines `access-token` as a plain yargs
// `demandOption` string flag with NO `env:` binding and no fallback to
// `process.env` anywhere in its handler (read directly from that file's
// own source before writing this module, per this project's own
// bibliography-before-implementing rule — this is not an assumption from
// the tool's name or its docs' silence). rollbar-cli's own separate
// config-file/env-var support (`ROLLBAR_ACCESS_TOKEN`, `~/.config/
// rollbar/config.yaml`, documented on a DIFFERENT, unofficial
// `rollbar whoami`-style CLI this port did not verify as the same
// project) is NOT wired into notify-deploy's own argument parsing at
// all — only `--access-token` reaches Deployer(). So this module has no
// choice but to place the token on the command line for every
// invocation — a genuine, unavoidable deviation from this project's hard
// "no secrets in argv" rule, verified against the CLI's own source
// rather than assumed, and documented here exactly as the batch
// instructions asked for when this situation turns out to be real.
//
// # url — no effect
//
// rollbar-cli's own notify-deploy has no flag to override Rollbar's API
// origin at all (verified: command.js's own option list above is
// exhaustive) — real rollbar_deployment.py's own `url` argument is
// therefore accepted for shape compatibility only, with NO EFFECT.
//
// # validate_certs — no effect
//
// rollbar-cli exposes no TLS-verification-disabling flag either;
// validate_certs is accepted for shape compatibility only, same as
// honeybadger_deployment.go's own documented gap.
//
// # check_mode — not implemented
//
// See honeybadger_deployment.go's own doc comment: this port's module
// signature has no check-mode plumbing, so every invocation actually
// runs `rollbar-cli notify-deploy`.
func moduleRollbarDeployment(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, errArg("rollbar_deployment: %v", err)
	}
	environment, err := requireString(args, "environment")
	if err != nil {
		return Result{}, errArg("rollbar_deployment: %v", err)
	}
	revision, err := requireString(args, "revision")
	if err != nil {
		return Result{}, errArg("rollbar_deployment: %v", err)
	}
	user := argString(args, "user", "")
	rollbarUser := argString(args, "rollbar_user", "")
	comment := argString(args, "comment", "")

	if _, err := run(ctx, conn, "command -v rollbar-cli"); err != nil {
		return Fail("rollbar_deployment: the rollbar-cli binary (Rollbar's own official CLI, npm package " +
			"rollbar-cli) is required on the target and was not found in PATH — this port shells out to it " +
			"rather than POSTing to the Rollbar deploy API directly; see this module's own doc comment"), nil
	}

	// --access-token genuinely has no environment-variable alternative in
	// rollbar-cli's own notify-deploy (see this module's own doc comment)
	// — this is the one case in this batch where the token cannot avoid
	// argv.
	argv := []string{
		"rollbar-cli", "notify-deploy",
		"--access-token", token,
		"--environment", environment,
		"--code-version", revision,
	}
	if user != "" {
		argv = append(argv, "--local-username", user)
	}
	if rollbarUser != "" {
		argv = append(argv, "--rollbar-username", rollbarUser)
	}
	if comment != "" {
		argv = append(argv, "--comment", comment)
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	res, err := runStatus(ctx, conn, strings.Join(quoted, " "))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail(fmt.Sprintf("rollbar_deployment: notify-deploy failed: %s", msg)), nil
	}
	return Changed("Notified Rollbar about the deployment").WithExtra("environment", environment), nil
}
