package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHoneybadgerDeployment implements Ansible's
// `honeybadger_deployment` module via Honeybadger's own official `hb`
// CLI (github.com/honeybadger-io/cli, "Official CLI for interacting
// with the Honeybadger API") instead of real honeybadger_deployment.py's
// own hand-rolled `fetch_url` POST to
// https://api.honeybadger.io/v1/deploys — the same "shell out to the
// platform's own official CLI instead of an API client" precedent this
// port already uses elsewhere (see github_common.go's own doc comment
// for the fuller rationale).
//
// # Command mapping — verified directly against `hb`'s own Go source
// # (cmd/deploy.go, cmd/root.go on the main branch), not guessed
//
// `hb deploy` sends the EXACT same payload real honeybadger_deployment.py
// posts (`{"deploy": {"environment", "repository", "revision",
// "local_username"}}` to `<endpoint>/v1/deploys`), just via a `net/http`
// POST inside the `hb` binary instead of this module's own HTTP client.
// Its flags map one-for-one onto real honeybadger_deployment.py's own
// arguments:
//
//	real arg   -> hb flag           (verified in cmd/deploy.go's own
//	                                  deployCmd.Flags().StringVarP calls)
//	environment -> -e, --environment (required, matching real module)
//	repo        -> -r, --repository
//	revision    -> -v, --revision
//	user        -> -u, --user
//
// # Auth: token via the HONEYBADGER_API_KEY environment variable, never
// # argv
//
// `hb`'s own cmd/root.go calls `viper.SetEnvPrefix("HONEYBADGER")` +
// `viper.AutomaticEnv()`, binding a bare `api_key` viper key (also
// settable via its own global `--api-key` flag) to the
// HONEYBADGER_API_KEY environment variable automatically — confirmed
// directly in `hb`'s own source, not assumed. This port always exports
// real honeybadger_deployment.py's own required `token` argument as
// HONEYBADGER_API_KEY for the single `hb deploy` invocation, matching
// this project's hard "no secrets in argv" rule (never as `--api-key`
// on the command line).
//
// # `url` — mapped onto `hb`'s own `--endpoint` flag, with a real caveat
//
// Real honeybadger_deployment.py's own `url` argument defaults to the
// FULL deploys endpoint, "https://api.honeybadger.io/v1/deploys". `hb`'s
// own `--endpoint` flag instead takes just the API's BASE origin
// (cmd/deploy.go's own request construction: `fmt.Sprintf("%s/v1/deploys",
// apiEndpoint)` — verified in source), defaulting to
// "https://api.honeybadger.io" internally. So: when `url` is omitted or
// exactly equals real honeybadger_deployment.py's own default, this port
// does not pass --endpoint at all (letting `hb` use its own default);
// when a caller supplies a custom `url` this port strips a trailing
// "/v1/deploys" suffix (if present) and passes the remainder as
// --endpoint — a best-effort translation for the one shape real callers
// actually use (a full deploys-endpoint URL), not a general URL rewriter.
//
// # validate_certs — accepted for shape compatibility, no effect
//
// `hb`'s own source (cmd/root.go, cmd/deploy.go) exposes no flag to
// disable TLS certificate validation at all — its HTTP client is a bare
// `&http.Client{}` with Go's default TLS behavior. real
// honeybadger_deployment.py's own validate_certs=false lets a self-signed
// endpoint through; this port has no way to replicate that through `hb`,
// so validate_certs is accepted (for argument-shape compatibility) but
// has NO EFFECT — an honest gap, not a silent one.
//
// # check_mode — not implemented
//
// Real honeybadger_deployment.py supports check_mode=full (it reports
// changed=true without ever calling the API). This port's module
// signature has no check-mode plumbing at all, matching every other
// module in this package that documents the same simplification (e.g.
// cronvar.go's own doc comment) — every invocation of this module
// actually runs `hb deploy`.
func moduleHoneybadgerDeployment(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, errArg("honeybadger_deployment: %v", err)
	}
	environment, err := requireString(args, "environment")
	if err != nil {
		return Result{}, errArg("honeybadger_deployment: %v", err)
	}
	user := argString(args, "user", "")
	repo := argString(args, "repo", "")
	revision := argString(args, "revision", "")
	url := argString(args, "url", "https://api.honeybadger.io/v1/deploys")

	if _, err := run(ctx, conn, "command -v hb"); err != nil {
		return Fail("honeybadger_deployment: the hb binary (Honeybadger's own official CLI, " +
			"github.com/honeybadger-io/cli) is required on the target and was not found in PATH — this port " +
			"shells out to it rather than POSTing to the Honeybadger deploys API directly; see this module's " +
			"own doc comment"), nil
	}

	argv := []string{"hb", "deploy", "-e", environment}
	if repo != "" {
		argv = append(argv, "-r", repo)
	}
	if revision != "" {
		argv = append(argv, "-v", revision)
	}
	if user != "" {
		argv = append(argv, "-u", user)
	}
	if url != "" && url != "https://api.honeybadger.io/v1/deploys" {
		argv = append(argv, "--endpoint", strings.TrimSuffix(url, "/v1/deploys"))
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := "HONEYBADGER_API_KEY=" + shellQuote(token) + " " + strings.Join(quoted, " ")

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail(fmt.Sprintf("honeybadger_deployment: hb deploy failed: %s", msg)), nil
	}
	return Changed("Notified Honeybadger about the deployment").WithExtra("environment", environment), nil
}
