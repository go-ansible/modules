package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGitlabProjectApprovals implements Ansible's
// `gitlab_project_approvals` (community.general) module: reads and
// updates a project's merge-request approval configuration, via `glab
// api` against GitLab's own GET/POST /projects/:id/approvals — see
// gitlab_common.go's own doc comment for the `glab` substitution and
// its accepted-but-inert api_*/validate_certs/ca_path arguments. `glab`
// has no dedicated subcommand for this resource. Deviation: GitLab's
// own API documents this endpoint's update verb as POST, not PUT (the
// verb every other settings-object resource in this batch uses) —
// verified against the real module's own python-gitlab call
// (`project.approvals.set_approvers`/`save()`, which itself POSTs), not
// guessed.
//
// Args: project (required); approvals_before_merge (int, deprecated in
// GitLab 12.3 but still accepted); reset_approvals_on_push;
// disable_overriding_approvers_per_merge_request;
// merge_requests_author_approval;
// merge_requests_disable_committers_approval;
// require_password_to_approve (deprecated in GitLab 16.9);
// require_reauthentication_to_approve; selective_code_owner_removals —
// all bool, all optional; an omitted argument is left at its current
// value (this module reads the current settings first and only sends
// the fields the task actually specifies, so a task naming a subset of
// these options never resets the others).
//
// Idempotent: compares each specified argument against the freshly-read
// current settings; a POST is issued (Changed=true) only when at least
// one differs.
//
// Extra["project_approvals"]: the settings object after this module
// ran (the freshly re-read current object if nothing changed, the
// POST's own response body if something did), matching real
// gitlab_project_approvals's own documented return.
func moduleGitlabProjectApprovals(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_project_approvals"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	path := "projects/" + glabEncodeID(project) + "/approvals"

	var current map[string]any
	gres, err := glabAPIJSON(ctx, conn, "GET", path, nil, false, &current)
	if err != nil {
		return Result{}, err
	}
	if gres.RC != 0 {
		return Fail("gitlab_project_approvals: unable to read approval settings: " + glabErrMsg(gres)), nil
	}

	fieldMap := map[string]string{
		"approvals_before_merge":                         "approvals_before_merge",
		"reset_approvals_on_push":                        "reset_approvals_on_push",
		"disable_overriding_approvers_per_merge_request": "disable_overriding_approvers_per_merge_request",
		"merge_requests_author_approval":                 "merge_requests_author_approval",
		"merge_requests_disable_committers_approval":     "merge_requests_disable_committers_approval",
		"require_password_to_approve":                    "require_password_to_approve",
		"require_reauthentication_to_approve":            "require_reauthentication_to_approve",
		"selective_code_owner_removals":                  "selective_code_owner_removals",
	}

	body := map[string]any{}
	for argKey, apiKey := range fieldMap {
		v, ok := args[argKey]
		if !ok {
			continue
		}
		var want any
		if argKey == "approvals_before_merge" {
			want = argInt(args, argKey, 0)
		} else {
			want = argBool(args, argKey, false)
		}
		have, existed := current[apiKey]
		if !existed || !jsonScalarEqual(want, have) {
			body[apiKey] = want
		}
		_ = v
	}

	if len(body) == 0 {
		return Ok("already up to date").WithExtra("project_approvals", current), nil
	}

	var updated map[string]any
	ures, err := glabAPIJSON(ctx, conn, "POST", path, body, false, &updated)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return Fail("gitlab_project_approvals: unable to update approval settings: " + glabErrMsg(ures)), nil
	}
	return Changed("approval settings updated").WithExtra("project_approvals", updated), nil
}

// jsonScalarEqual compares a Go bool/int against a value freshly
// decoded from JSON into an `any` (so a number always arrives as
// float64) — used by moduleGitlabProjectApprovals to diff its own
// desired scalars against the API's own current settings object.
func jsonScalarEqual(want, have any) bool {
	switch w := want.(type) {
	case bool:
		h, ok := have.(bool)
		return ok && h == w
	case int:
		switch h := have.(type) {
		case float64:
			return int(h) == w
		case int:
			return h == w
		}
		return false
	default:
		return want == have
	}
}
