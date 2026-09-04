package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file (github_webhook.go/github_webhook_info.go) manages GitHub
// repository webhooks via `gh api` directly against GitHub's own REST
// endpoints (`repos/{owner}/{repo}/hooks`), since `gh` has no
// dedicated `gh webhook` subcommand family the way it has `gh secret`/
// `gh ssh-key`/`gh release` — see github_common.go's own doc comment
// for why this port substitutes `gh` for real github_webhook's own
// PyGithub-based REST API calls generally, and this note for why THIS
// pair specifically goes through `gh api` (a thin authenticated-HTTP
// wrapper) rather than a higher-level `gh` subcommand.
//
// `gh api`'s own `-f`/`-F` field flags build the request body: `-f`
// (raw-field) always sends a literal string; `-F` (typed field)
// auto-converts `true`/`false`/`null`/bare integers to their JSON
// types and supports `key[]=value` repeated-array syntax. This port
// uses `-F` for `active` (bool) and `events[]` (array), and `-f` for
// every string-valued config[...] field — including
// config[insecure_ssl], which GitHub's own API expects as the STRING
// "0"/"1", not a JSON boolean or integer, matching real
// _create_hook_config's own `"1" if ... else "0"` exactly (a `-F`
// field would silently send it as a JSON integer 0/1 instead).

// ghWebhookHook mirrors the fields this port reads back from GitHub's
// own hook JSON resource (both for github_webhook.go's own lookup and
// github_webhook_info.go's own listing).
type ghWebhookHook struct {
	ID     int  `json:"id"`
	Active bool `json:"active"`
	Events []string
	Config struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		InsecureSSL string `json:"insecure_ssl"`
		Secret      string `json:"secret"`
	} `json:"config"`
	LastResponse map[string]any `json:"last_response"`
}

// ghWebhookList lists spec's webhooks via `gh api repos/{spec}/hooks`.
// Deviation — no pagination: real github_webhook_info's own PyGithub
// get_hooks() transparently paginates; this port issues a single `gh
// api` call (GitHub's own default page size, 30), matching the
// pragmatic scope every list-returning github_*.go module in this
// batch accepts rather than implementing `gh api --paginate`'s own
// multi-JSON-document output reassembly for a resource (webhooks per
// repo) that in practice is never anywhere near 30.
func ghWebhookList(ctx context.Context, conn remoteexec.Connection, token, spec string) ([]ghWebhookHook, remoteexec.Result, error) {
	var hooks []ghWebhookHook
	res, err := ghRunJSON(ctx, conn, token, &hooks, "api", "repos/"+spec+"/hooks")
	return hooks, res, err
}

// moduleGithubWebhook implements Ansible's `github_webhook`
// (community.general) module: creates, updates, or deletes a GitHub
// repository webhook — see this file's own doc comment for the `gh
// api` substitution.
//
// Args: repository (required, alias repo) — full "owner/repo" name,
// used as-is (unlike github_deploy_key/github_release, this module's
// real counterpart already takes the combined form, so no owner/repo
// join is needed here); url (required) — the payload delivery URL,
// also this port's own lookup key (see below); content_type (form|
// json, default form); secret (optional); insecure_ssl (bool, default
// false); events ([]string, required unless state=absent); active
// (bool, default true); state (present|absent, default present); user
// (required) — accepted, no effect (see github_common.go; real
// github_webhook's own PyGithub basic-auth/token login has no `gh`
// equivalent, and this module does not even reach `gh` without SOME
// prior `gh auth login` on the target regardless of what `user` says);
// password — accepted, no effect; token — wired into GH_TOKEN;
// github_url — accepted, no effect, same reasoning as
// github_deploy_key's github_url.
//
// A hook is looked up by its config.url matching `url` exactly — the
// same key real github_webhook.py's own `for hook in repo.get_hooks():
// if hook.config.get("url") == module.params["url"]` uses, not by hook
// ID (which a caller has no way to supply as a stable identifier
// across runs, matching every other github_*.go module in this batch's
// own convention of never asking a playbook to track an
// API-server-assigned ID between runs).
//
// state=present, no existing hook: `gh api -X POST repos/{repo}/hooks`
// with name=web, active, events[], and config[...] fields — Changed=
// true, Extra["hook_id"] = the created hook's own id.
//
// state=present, existing hook: this port compares the fetched hook's
// own active/events (order-insensitive)/config.url/config.content_type/
// config.insecure_ssl against the requested values FIRST, and issues
// `gh api -X PATCH repos/{repo}/hooks/{id}` (Changed=true) only when at
// least one differs — see this file's own doc comment for why this is
// a deliberate improvement over real update_hook, which always calls
// hook.edit() unconditionally on every run and reports Changed as
// PyGithub's own generic hook.update() boolean (frequently True even
// with no real difference, since PyGithub's update() reflects whether
// the hook's OWN freshly-refetched ETag/data changed at all, not
// specifically whether this module's edit did anything). GitHub's API
// never returns a hook's own `secret` value back (it's write-only, the
// same reasoning github_secrets.go's own doc comment gives for
// state=present always being Changed there) — so when `secret` is
// given (non-empty), this port cannot compare it and always treats
// that as a difference, matching this port's own house convention
// (see consul_kv.go's "no signal, so this port computes its own diff"
// elsewhere) of being conservative rather than silently assuming an
// unobservable value already matches.
//
// state=absent: DELETE the found hook (Changed=true); if none is
// found, Changed=false — matching real main()'s own `# else, there is
// no hook and we want there to be no hook` no-op comment exactly.
func moduleGithubWebhook(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	spec, err := requireString(args, "repository")
	if err != nil {
		return Result{}, err
	}
	url, err := requireString(args, "url")
	if err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "user"); err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("github_webhook: state must be one of present, absent, got %q", state)
	}
	events := argStringList(args, "events")
	if state == "present" && len(events) == 0 {
		return Result{}, errArg("github_webhook: events is required when state=present")
	}
	token := argString(args, "token", "")

	hooks, listRes, err := ghWebhookList(ctx, conn, token, spec)
	if err != nil {
		return Result{}, err
	}
	if listRes.RC != 0 {
		return Fail("github_webhook: unable to get hooks from repository " + spec + ": " + ghStderr(listRes)), nil
	}
	var found *ghWebhookHook
	for i := range hooks {
		if hooks[i].Config.URL == url {
			found = &hooks[i]
			break
		}
	}

	if found == nil && state == "present" {
		fields := ghWebhookConfigFields(args)
		createArgs := append([]string{"api", "-X", "POST", "repos/" + spec + "/hooks", "-f", "name=web"}, fields...)
		var created ghWebhookHook
		res, jerr := ghRunJSON(ctx, conn, token, &created, createArgs...)
		if jerr != nil {
			return Result{}, jerr
		}
		if res.RC != 0 {
			return Fail("github_webhook: unable to create hook for repository " + spec + ": " + ghStderr(res)), nil
		}
		return Changed("").WithExtra("hook_id", created.ID), nil
	}

	if found != nil && state == "absent" {
		res, err := ghRun(ctx, conn, token, nil, "api", "-X", "DELETE", "repos/"+spec+"/hooks/"+strconv.Itoa(found.ID))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("github_webhook: unable to delete hook from repository " + spec + ": " + ghStderr(res)), nil
		}
		return Changed(""), nil
	}

	if found != nil && state == "present" {
		if !ghWebhookNeedsUpdate(found, args) {
			return Ok("").WithExtra("hook_id", found.ID), nil
		}
		fields := ghWebhookConfigFields(args)
		editArgs := append([]string{"api", "-X", "PATCH", "repos/" + spec + "/hooks/" + strconv.Itoa(found.ID), "-f", "name=web"}, fields...)
		res, err := ghRun(ctx, conn, token, nil, editArgs...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("github_webhook: unable to modify hook for repository " + spec + ": " + ghStderr(res)), nil
		}
		return Changed("").WithExtra("hook_id", found.ID), nil
	}

	// found == nil && state == absent: nothing to do.
	return Ok(""), nil
}

// ghWebhookConfigFields renders the `-F`/`-f` field flags shared by
// hook create and edit — see this file's own doc comment for why
// insecure_ssl uses `-f` (a literal string), not `-F`.
func ghWebhookConfigFields(args map[string]any) []string {
	active := argBool(args, "active", true)
	activeStr := "false"
	if active {
		activeStr = "true"
	}
	insecureSSL := "0"
	if argBool(args, "insecure_ssl", false) {
		insecureSSL = "1"
	}
	fields := []string{
		"-F", "active=" + activeStr,
		"-f", "config[url]=" + argString(args, "url", ""),
		"-f", "config[content_type]=" + argString(args, "content_type", "form"),
		"-f", "config[insecure_ssl]=" + insecureSSL,
	}
	if secret := argString(args, "secret", ""); secret != "" {
		fields = append(fields, "-f", "config[secret]="+secret)
	}
	for _, e := range argStringList(args, "events") {
		fields = append(fields, "-F", "events[]="+e)
	}
	return fields
}

// ghWebhookNeedsUpdate reports whether found's own fields differ from
// what args request — see moduleGithubWebhook's own doc comment for
// why this port computes its own diff rather than always PATCHing.
func ghWebhookNeedsUpdate(found *ghWebhookHook, args map[string]any) bool {
	if found.Active != argBool(args, "active", true) {
		return true
	}
	if found.Config.ContentType != argString(args, "content_type", "form") {
		return true
	}
	wantInsecure := "0"
	if argBool(args, "insecure_ssl", false) {
		wantInsecure = "1"
	}
	if found.Config.InsecureSSL != wantInsecure {
		return true
	}
	if argString(args, "secret", "") != "" {
		// GitHub never returns a hook's own secret value — cannot
		// compare, so any requested secret is treated as a difference.
		return true
	}
	if !stringSetEqual(found.Events, argStringList(args, "events")) {
		return true
	}
	return false
}
