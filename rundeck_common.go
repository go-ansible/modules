package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the four rundeck_* modules in this batch
// share: shelling out to `rd` (Rundeck's own official CLI, published by
// Rundeck as "rundeck-cli") instead of driving the Rundeck HTTP REST
// API directly the way every real rundeck_* (community.general) module
// does (module_utils/_rundeck.py's api_request, a thin fetch_url
// wrapper that always sends `Accept: application/json` and always
// expects a JSON response body) — the same "shell out to the
// platform's own official CLI instead of an API client" precedent this
// port already uses for Consul/Redis/Terraform/Icinga2/Kopia/GitHub/
// GitLab/Keycloak in prior batches.
//
// # Auth: a genuine improvement over the glab/kcadm precedent
//
// Unlike `glab`/`kcadm.sh` (which have NO per-invocation login concept
// at all — see gitlab_common.go's/keycloak_common.go's own doc
// comments), `rd` DOES: it is officially documented (rundeck-cli's own
// docs/configuration.md) to read RD_URL and RD_TOKEN from the
// environment for a single invocation, with no prior `rd config`/`rd
// auth` needed at all when those are set. That maps directly onto real
// rundeck_* modules' own REQUIRED url/api_token arguments (both marked
// required in every real rundeck_* module's own argument_spec), so
// this port wires them in properly — as RD_URL/RD_TOKEN environment
// variables for that single `rd` invocation only, NEVER as a
// command-line flag, matching this project's own hard "no secrets in
// argv" rule (see redis.go's own REDISCLI_AUTH precedent). This is a
// real per-invocation auth path, not a documented gap.
//
// The rest of every real rundeck_* module's own connection arguments —
// url_username/url_password/use_gssapi/client_cert/client_key/force/
// force_basic_auth/http_agent/use_proxy/validate_certs, all inherited
// from Ansible's own generic `ansible.builtin.url` fragment for driving
// fetch_url directly — have no `rd` equivalent (rd's only two
// documented credential paths are RD_TOKEN and RD_USER/RD_PASSWORD; the
// rest are HTTP-transport knobs specific to Ansible's own urllib
// wrapper). Every rundeck_* module in this batch still accepts them all
// (for argument-shape compatibility with real playbooks) but they have
// NO EFFECT on this port's behavior — a deliberate, honestly-documented
// gap, not a silent misinterpretation.
//
// # JSON output: RD_FORMAT=json
//
// `rd` has no `gh api`/`glab api`-style generic REST passthrough
// subcommand — verified against rundeck-cli's own docs/commands.md,
// which lists no such command. Instead, this port sets RD_FORMAT=json
// (rundeck-cli's own documented global output-format override,
// confirmed via a real rundeck-cli GitHub issue —
// github.com/rundeck/rundeck-cli/issues/192 — which shows RD_FORMAT=json
// producing valid JSON for `rd executions` commands, while also noting
// its shape is not always as richly structured as the raw REST response
// would be for every command) alongside every invocation, and decodes
// whatever JSON that command's own implementation happens to emit. This
// port's own rd binary was not available to exercise live in this
// sandbox; the exact flag surface below (rdRun's argv construction) is
// applied on the strength of rundeck-cli's own published command
// reference (docs.rundeck.com/docs/rd-cli/commands.html) and its own
// GitHub source tree, not verified against a live `rd` binary — the
// same honesty this port's "read the reference before implementing"
// rule asks for when a live check truly isn't possible (matching
// gitlab_common.go's own identical stance on `glab api`).
//
// # No generic API passthrough
//
// Because `rd` has dedicated subcommands for everything this batch's
// four modules need (projects, acls, run, executions), none of them
// falls back to a raw HTTP call the way, say, glab_common.go's modules
// fall back to `glab api` for resources `glab` has no subcommand for.

// rdRequireBinary fails cleanly (Result{Failed:true}, not a Go error)
// if the real `rd` CLI is not on the target's PATH.
func rdRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v rd"); err != nil {
		return Fail(fmt.Sprintf("%s: the rd binary (Rundeck's own official CLI, package rundeck-cli) is required "+
			"on the target and was not found in PATH — this port shells out to it rather than speaking the "+
			"Rundeck REST API directly; see rundeck_common.go's own doc comment for how url/api_token are wired "+
			"in as RD_URL/RD_TOKEN for each invocation", moduleName)), false
	}
	return Result{}, true
}

// rdRun runs one `rd` invocation on conn, with RD_FORMAT=json always
// set and RD_URL/RD_TOKEN set (only for that single command, never on
// the command line — see this file's own doc comment) when url/token
// are non-empty.
func rdRun(ctx context.Context, conn remoteexec.Connection, url, token string, argv ...string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := "RD_FORMAT=json"
	if url != "" {
		cmd += " RD_URL=" + shellQuote(url)
	}
	if token != "" {
		cmd += " RD_TOKEN=" + shellQuote(token)
	}
	cmd += " rd " + strings.Join(quoted, " ")
	return conn.Exec(ctx, cmd, nil)
}

// rdRunJSON runs argv (via rdRun) and, on a zero exit with non-empty
// stdout, decodes it as JSON into out. Returns the raw Result too, so a
// caller can still inspect RC/Stderr on failure.
func rdRunJSON(ctx context.Context, conn remoteexec.Connection, url, token string, out any, argv ...string) (remoteexec.Result, error) {
	res, err := rdRun(ctx, conn, url, token, argv...)
	if err != nil {
		return res, err
	}
	if res.RC != 0 || strings.TrimSpace(res.Stdout) == "" {
		return res, nil
	}
	if jerr := json.Unmarshal([]byte(res.Stdout), out); jerr != nil {
		return res, fmt.Errorf("decoding rd %s response: %w", strings.Join(argv, " "), jerr)
	}
	return res, nil
}

// rdErrMsg builds a Fail() message from a non-zero rd invocation,
// preferring stderr but falling back to stdout.
func rdErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// rdAuth pulls the (url, api_token) pair every rundeck_* module in this
// batch requires out of args — both are marked required by every real
// rundeck_* module's own argument_spec (api_token aliased "token" on
// rundeck_project/rundeck_acl_policy, matching real stacki_host.py's
// own aliasing).
func rdAuth(args map[string]any) (url, token string, err error) {
	url, err = requireString(args, "url")
	if err != nil {
		return "", "", err
	}
	token = argString(args, "api_token", argString(args, "token", ""))
	if token == "" {
		return "", "", errArg("missing required argument: api_token")
	}
	return url, token, nil
}
