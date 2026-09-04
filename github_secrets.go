package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGithubSecrets implements Ansible's `github_secrets`
// (community.general) module: creates, updates, or deletes a GitHub
// Actions secret at the repository or organization level, via `gh
// secret set`/`delete` — see github_common.go's own doc comment for
// why this port substitutes the `gh` CLI for real github_secrets' own
// direct GitHub REST API calls (which encrypt the secret value
// client-side with pynacl against the repository/organization's own
// public key before sending it; `gh secret set` performs that same
// libsodium sealed-box encryption internally, so the observable
// server-side result is identical even though this port never handles
// the public key or encryption itself).
//
// Args: organization (required, aliases org/username); repository
// (optional, alias repo) — when set, the secret is repository-scoped
// (`gh secret set/delete -R organization/repository`); when unset,
// it's organization-scoped (`-o organization`); key (the secret name);
// value (required for state=present) — piped to `gh secret set`'s own
// stdin via `--body`... — see below; visibility (all|private|selected,
// required for state=present when repository is unset, matching real
// github_secrets' own required_if-equivalent validation) — passed as
// `gh secret set`'s own `--visibility`; state (present|absent, default
// present); token (required) — wired into GH_TOKEN (see
// github_common.go), matching real github_secrets' own required token
// argument.
//
// Deviation — api_url is accepted for argument-shape compatibility but
// has no effect, same reasoning as github_deploy_key's github_url (see
// that module's own doc comment).
//
// Deviation — secret VALUE handling: real github_secrets passes value
// on the command line's own --body-equivalent is actually an HTTPS PUT
// body; this port passes value to `gh secret set`'s own `-b/--body`
// flag rather than piping it over stdin, because `-b` is documented as
// reading the value directly (not a file path) — still never appearing
// in the target's process listing the way this project's hard "no
// secrets in argv" rule cares about would require checking: `gh secret
// set`'s own -b flag value IS visible in `ps` output on the target for
// the brief life of the process, unlike REDISCLI_AUTH/GH_TOKEN-style
// environment-variable secrets. This port instead pipes value over
// stdin (gh secret set's own documented no-flag default: "reads from
// standard input if not specified"), keeping it out of argv entirely,
// matching the project's own hard rule more faithfully than real
// github_secrets' own HTTPS-body approach needed to worry about.
//
// state=present ALWAYS issues `gh secret set` and ALWAYS reports
// Changed=true, matching real github_secrets' own upsert_secret, which
// never reads back a secret's current value first (GitHub's own API
// makes that impossible — secret values are write-only) — there is no
// idempotency check to perform. state=absent issues `gh secret
// delete`; a failure whose own error text contains "HTTP 404" (see
// ghNotFound) is treated as "the secret didn't exist" (Changed=false),
// matching real delete_secret's own HTTPStatus.NOT_FOUND-is-not-an-
// error handling; any other failure is a genuine Fail.
func moduleGithubSecrets(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	org, err := requireString(args, "organization")
	if err != nil {
		return Result{}, err
	}
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	repo := argString(args, "repository", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("github_secrets: state must be one of present, absent, got %q", state)
	}
	scopeArgs := []string{"-R", org + "/" + repo}
	if repo == "" {
		scopeArgs = []string{"-o", org}
	}

	switch state {
	case "present":
		value := argString(args, "value", "")
		if value == "" {
			return Result{}, errArg("github_secrets: value is required when state=present")
		}
		visibility := argString(args, "visibility", "")
		if repo == "" && visibility == "" {
			return Result{}, errArg("github_secrets: visibility is required when state=present and repository is not set")
		}
		setArgs := append([]string{"secret", "set", key}, scopeArgs...)
		if repo == "" {
			setArgs = append(setArgs, "--visibility", visibility)
		}
		res, err := ghRun(ctx, conn, token, strings.NewReader(value), setArgs...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("github_secrets: failed to set secret " + key + ": " + ghStderr(res)), nil
		}
		result := map[string]any{"response": "Secret created", "msg": ghStderr(res)}
		return Changed("").WithExtra("result", result), nil

	default: // absent
		delArgs := append([]string{"secret", "delete", key}, scopeArgs...)
		res, err := ghRun(ctx, conn, token, nil, delArgs...)
		if err != nil {
			return Result{}, err
		}
		if res.RC == 0 {
			result := map[string]any{"response": "Secret deleted", "msg": ghStderr(res)}
			return Changed("").WithExtra("result", result), nil
		}
		if ghNotFound(res) {
			result := map[string]any{"response": "Secret not found", "msg": ghStderr(res)}
			return Ok("").WithExtra("result", result), nil
		}
		return Fail("github_secrets: failed to delete secret " + key + ": " + ghStderr(res)), nil
	}
}
