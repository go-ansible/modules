package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabHookObj is one entry of `glab api projects/:id/hooks`'s own
// JSON array/object — only the fields this module reads or writes.
// Deliberately has no Token field: GitLab's own API never returns a
// webhook's secret token, matching real gitlab_hook's own documented
// note ("It shows up in the X-GitLab-Token HTTP request header ... it
// cannot be retrieved from GitLab").
type gitlabHookObj struct {
	ID                     int    `json:"id"`
	URL                    string `json:"url"`
	PushEvents             bool   `json:"push_events"`
	IssuesEvents           bool   `json:"issues_events"`
	MergeRequestsEvents    bool   `json:"merge_requests_events"`
	TagPushEvents          bool   `json:"tag_push_events"`
	NoteEvents             bool   `json:"note_events"`
	JobEvents              bool   `json:"job_events"`
	PipelineEvents         bool   `json:"pipeline_events"`
	WikiPageEvents         bool   `json:"wiki_page_events"`
	ReleasesEvents         bool   `json:"releases_events"`
	PushEventsBranchFilter string `json:"push_events_branch_filter"`
	EnableSSLVerification  bool   `json:"enable_ssl_verification"`
	CustomWebhookTemplate  string `json:"custom_webhook_template"`
}

// moduleGitlabHook implements Ansible's `gitlab_hook`
// (community.general) module: adds, updates, or removes a project
// webhook, via `glab api` against GitLab's own GET/POST/PUT/DELETE
// /projects/:id/hooks(/:id) — see gitlab_common.go's own doc comment
// for the `glab` substitution and its accepted-but-inert
// api_*/validate_certs/ca_path arguments. `glab` has no dedicated
// webhook subcommand.
//
// Args: project (required); hook_url (required) — this port's (and
// real gitlab_hook's own, per its own doc: "used as the primary key for
// updates and deletion") matching key, since a project's hooks have no
// other natural key; push_events (default true);
// issues_events/merge_requests_events/tag_push_events/note_events/
// job_events/pipeline_events/wiki_page_events (all default false);
// releases_events (bool, no default — omitted from every request when
// not specified, matching real gitlab_hook's own `default: null`);
// push_events_branch_filter (default ""); hook_validate_certs (aliased
// enable_ssl_verification, default false) — sent as the API's own
// enable_ssl_verification field; custom_webhook_template; token
// (secret — write-only, never read back, see gitlabHookObj's own doc
// comment) — matching real gitlab_hook's own documented behavior, a
// non-empty token ALWAYS results in Changed=true, since this port (like
// real gitlab_hook itself) has no way to compare it against the
// existing hook's own value; state (present|absent, default present).
//
// state=absent: a hook with matching hook_url is deleted; no match is a
// no-op. state=present: no match -> POST with every field above. A
// match is compared field-by-field (excluding token, per above); PUT
// only when at least one differs (or token is set), else a no-op.
//
// Extra["hook"]: the hook object, present whenever state=present,
// matching real gitlab_hook's own documented return.
func moduleGitlabHook(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_hook"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	hookURL, err := requireString(args, "hook_url")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_hook: state must be one of present, absent, got %q", state)
	}
	base := "projects/" + glabEncodeID(project) + "/hooks"

	var hooks []gitlabHookObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base+"?per_page=100", nil, true, &hooks)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_hook: unable to list hooks: " + glabErrMsg(lres)), nil
	}
	var existing *gitlabHookObj
	for i := range hooks {
		if hooks[i].URL == hookURL {
			existing = &hooks[i]
			break
		}
	}

	if state == "absent" {
		if existing == nil {
			return Ok(hookURL + " already absent"), nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(existing.ID), nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_hook: unable to delete " + hookURL + ": " + glabErrMsg(dres)), nil
		}
		return Changed(hookURL + " deleted"), nil
	}

	hookValidateCerts := argBool(args, "hook_validate_certs", argBool(args, "enable_ssl_verification", false))
	body := map[string]any{
		"url":                       hookURL,
		"push_events":               argBool(args, "push_events", true),
		"issues_events":             argBool(args, "issues_events", false),
		"merge_requests_events":     argBool(args, "merge_requests_events", false),
		"tag_push_events":           argBool(args, "tag_push_events", false),
		"note_events":               argBool(args, "note_events", false),
		"job_events":                argBool(args, "job_events", false),
		"pipeline_events":           argBool(args, "pipeline_events", false),
		"wiki_page_events":          argBool(args, "wiki_page_events", false),
		"push_events_branch_filter": argString(args, "push_events_branch_filter", ""),
		"enable_ssl_verification":   hookValidateCerts,
	}
	if _, ok := args["releases_events"]; ok {
		body["releases_events"] = argBool(args, "releases_events", false)
	}
	if t := argString(args, "custom_webhook_template", ""); t != "" {
		body["custom_webhook_template"] = t
	}
	token := argString(args, "token", "")
	if token != "" {
		body["token"] = token
	}

	if existing == nil {
		var created gitlabHookObj
		cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, &created)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("gitlab_hook: unable to create " + hookURL + ": " + glabErrMsg(cres)), nil
		}
		return Changed(hookURL+" created").WithExtra("hook", created), nil
	}

	diff := token != "" ||
		existing.PushEvents != argBool(args, "push_events", true) ||
		existing.IssuesEvents != argBool(args, "issues_events", false) ||
		existing.MergeRequestsEvents != argBool(args, "merge_requests_events", false) ||
		existing.TagPushEvents != argBool(args, "tag_push_events", false) ||
		existing.NoteEvents != argBool(args, "note_events", false) ||
		existing.JobEvents != argBool(args, "job_events", false) ||
		existing.PipelineEvents != argBool(args, "pipeline_events", false) ||
		existing.WikiPageEvents != argBool(args, "wiki_page_events", false) ||
		existing.PushEventsBranchFilter != argString(args, "push_events_branch_filter", "") ||
		existing.EnableSSLVerification != hookValidateCerts ||
		(argString(args, "custom_webhook_template", "") != "" && !strings.EqualFold(existing.CustomWebhookTemplate, argString(args, "custom_webhook_template", "")))

	if !diff {
		return Ok(hookURL+" already up to date").WithExtra("hook", *existing), nil
	}
	var updated gitlabHookObj
	ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+strconv.Itoa(existing.ID), body, false, &updated)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return Fail("gitlab_hook: unable to update " + hookURL + ": " + glabErrMsg(ures)), nil
	}
	return Changed(hookURL+" updated").WithExtra("hook", updated), nil
}
