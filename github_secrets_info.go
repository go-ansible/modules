package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGithubSecretsInfo implements Ansible's `github_secrets_info`
// (community.general) module: lists the names of GitHub Actions
// secrets at the repository or organization level, via `gh secret
// list` — see github_common.go's own doc comment for why this port
// substitutes the `gh` CLI for real github_secrets_info's own direct
// GitHub REST API calls.
//
// Args: organization (required, aliases org/username); repository
// (optional, alias repo) — repository-scoped when set (`-R
// organization/repository`), organization-scoped otherwise (`-o
// organization`); token (required) — wired into GH_TOKEN (see
// github_common.go).
//
// Deviation — api_url is accepted for argument-shape compatibility but
// has no effect, same reasoning as github_deploy_key's github_url (see
// that module's own doc comment).
//
// GitHub's own API (and `gh secret list` in turn) never returns a
// secret's VALUE, only its metadata — matching real
// github_secrets_info's own documented limitation exactly, not a gap
// this port introduces.
//
// Extra["secrets"]: one {name, updated_at} entry per secret, decoded
// from `gh secret list --json name,updatedAt`.
//
// Deviation — no created_at: real github_secrets_info's own return
// sample includes both created_at and updated_at (straight from
// GitHub's REST API's own secret resource, which has both fields).
// `gh secret list --json`'s own field set (verified directly against
// this batch's own locally installed `gh` binary: name,
// numSelectedRepos, selectedReposURL, updatedAt, visibility) has no
// createdAt at all — this port cannot populate it and omits it rather
// than fake a value.
func moduleGithubSecretsInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	org, err := requireString(args, "organization")
	if err != nil {
		return Result{}, err
	}
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	repo := argString(args, "repository", "")
	scopeArgs := []string{"-R", org + "/" + repo}
	if repo == "" {
		scopeArgs = []string{"-o", org}
	}

	var raw []struct {
		Name      string `json:"name"`
		UpdatedAt string `json:"updatedAt"`
	}
	listArgs := append([]string{"secret", "list", "--json", "name,updatedAt"}, scopeArgs...)
	res, err := ghRunJSON(ctx, conn, token, &raw, listArgs...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("github_secrets_info: failed to list secrets: " + ghStderr(res)), nil
	}
	secrets := make([]map[string]any, 0, len(raw))
	for _, s := range raw {
		secrets = append(secrets, map[string]any{"name": s.Name, "updated_at": s.UpdatedAt})
	}
	return Ok("").WithExtra("secrets", secrets), nil
}
