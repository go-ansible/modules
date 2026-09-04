package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGithubWebhookInfo implements Ansible's `github_webhook_info`
// (community.general) module: lists a GitHub repository's own
// webhooks, via `gh api repos/{owner}/{repo}/hooks` — see
// github_webhook.go's own doc comment for why this port goes through
// `gh api` rather than a dedicated `gh webhook` subcommand (there
// isn't one), and github_common.go's own doc comment for the general
// `gh` CLI substitution.
//
// Args: repository (required, alias repo) — full "owner/repo" name;
// user (required) — accepted, no effect (see github_common.go);
// password/token — accepted (token wired into GH_TOKEN); github_url —
// accepted, no effect, same reasoning as github_deploy_key's
// github_url.
//
// Extra["hooks"]: one entry per webhook, fields active, content_type,
// events, has_shared_secret, id, insecure_ssl, last_response, url —
// matching real github_webhook_info's own return sample exactly
// (has_shared_secret is derived here from whether GitHub's own hook
// JSON includes a non-empty config.secret — which it in practice never
// does, secrets being write-only, matching real github_webhook_info's
// own `bool(hook.config.get("secret"))`, itself effectively always
// False for the same reason).
func moduleGithubWebhookInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	spec, err := requireString(args, "repository")
	if err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "user"); err != nil {
		return Result{}, err
	}
	token := argString(args, "token", "")

	hooks, res, err := ghWebhookList(ctx, conn, token, spec)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("github_webhook_info: unable to get hooks from repository " + spec + ": " + ghStderr(res)), nil
	}

	out := make([]map[string]any, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, map[string]any{
			"id":                h.ID,
			"active":            h.Active,
			"content_type":      h.Config.ContentType,
			"events":            h.Events,
			"has_shared_secret": h.Config.Secret != "",
			"insecure_ssl":      h.Config.InsecureSSL,
			"url":               h.Config.URL,
			"last_response":     h.LastResponse,
		})
	}
	return Ok("").WithExtra("hooks", out), nil
}
