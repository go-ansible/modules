package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabRunnerListEntry is one entry of a runner-list endpoint's own
// JSON array (GET /runners/all, /runners, /groups/:id/runners,
// /projects/:id/runners all share this shape) — just enough to find a
// runner by its own description.
type gitlabRunnerListEntry struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
}

// gitlabRunnerDetail is `glab api runners/:id`'s own JSON object — only
// the fields this module reads or writes.
type gitlabRunnerDetail struct {
	ID             int      `json:"id"`
	Description    string   `json:"description"`
	Active         bool     `json:"active"`
	Paused         bool     `json:"paused"`
	Locked         bool     `json:"locked"`
	RunUntagged    bool     `json:"run_untagged"`
	TagList        []string `json:"tag_list"`
	AccessLevel    string   `json:"access_level"`
	MaximumTimeout int      `json:"maximum_timeout"`
	Token          string   `json:"token"`
}

// moduleGitlabRunner implements Ansible's `gitlab_runner`
// (community.general) module: registers, updates, or deletes a GitLab
// CI runner at the GitLab-Server side of runner registration — via
// `glab api` against GitLab's own runner endpoints (GET
// /runners/all|/runners|/groups/:id/runners|/projects/:id/runners to
// find one by description, POST /user/runners or legacy POST /runners
// to create, PUT /runners/:id to update, DELETE /runners/:id to
// delete) — see gitlab_common.go's own doc comment for the `glab`
// substitution and its accepted-but-inert api_*/validate_certs/ca_path
// arguments.
//
// `glab` has NO dedicated runner-registration subcommand (its own `glab
// ci` command family covers pipeline/job inspection, not runner
// lifecycle management) — verified against `glab`'s own published
// command reference before falling back, per this batch's own
// instructions, to `glab api` as the generic passthrough for this
// resource, the same way every other module in this batch does for a
// resource `glab` has no dedicated subcommand for. This is NOT the
// "truly not coverable" case the batch instructions asked to be
// stubbed honestly instead of faked: GitLab's runner-management REST
// endpoints this module drives (create/read/update/delete) are
// ordinary, currently-documented API calls `glab api` can issue like
// any other, unlike (for contrast) gitlab_project's avatar_path, which
// needs a real multipart upload no JSON-body `glab api --input -` call
// can perform and is skipped instead (see gitlab_project.go's own doc
// comment).
//
// Matching real gitlab_runner's own explicit note, this module (like
// the real one) does NOT run `gitlab-runner register` on any runner
// process itself — it only manages the runner's own registration
// record GitLab Server holds; Extra["runner"]'s own "token" field (only
// populated on creation) is what a follow-up task would feed to a
// separate `gitlab-runner register` invocation.
//
// Args: description (required, aliased name) — this port's (and real
// gitlab_runner's own, per its NOTES: "unique descriptions... used as
// key for idempotency") matching key; group XOR project XOR owned
// (mutually exclusive — selects a group-level, project-level, or
// current-user-owned runner scope; none of the three means an
// instance-level (admin) runner, listed via /runners/all);
// registration_token — if set, creates via the legacy POST /runners
// workflow (GitLab < 16.0); if unset, creates via POST /user/runners
// (GitLab >= 16.0's own new workflow); active/paused (mutually
// exclusive; active default true) — this port maps whichever is given
// to the API's own "paused" field (paused = !active) for both create
// and update; locked (default false); run_untagged (default true);
// tag_list ([]string); maximum_timeout (default 3600; 0 disables it);
// access_level (not_protected|ref_protected) with
// access_level_on_creation (bool, default true) gating whether it is
// sent at creation at all (matching real gitlab_runner's own doc:
// unset access_level_on_creation-at-creation-time means GitLab picks
// its own default, only applied on later updates); state (present|
// absent, default present).
//
// state=present: no runner with a matching description in scope ->
// created (registration_token workflow or /user/runners workflow, per
// above); Extra["runner"] holds the created object including its
// one-time-visible token. A match -> GET /runners/:id for full detail,
// diffed against active/paused/locked/run_untagged/tag_list/
// maximum_timeout/access_level (access_level only compared/sent when
// the task actually specifies it, matching every other optional-field
// module in this batch); PUT only if something differs.
// state=absent: a matching runner is deleted; no match is a no-op.
func moduleGitlabRunner(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_runner"); !ok {
		return res, nil
	}
	description := argStringAliased(args, "description", "name", "")
	if description == "" {
		return Result{}, errArg("gitlab_runner: missing required argument: description (or its alias name)")
	}
	group := argString(args, "group", "")
	project := argString(args, "project", "")
	owned := argBool(args, "owned", false)
	n := 0
	for _, v := range []bool{group != "", project != "", owned} {
		if v {
			n++
		}
	}
	if n > 1 {
		return Result{}, errArg("gitlab_runner: group, project, and owned are mutually exclusive")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_runner: state must be one of present, absent, got %q", state)
	}

	listPath := "runners/all?per_page=100"
	switch {
	case group != "":
		listPath = "groups/" + glabEncodeID(group) + "/runners?per_page=100"
	case project != "":
		listPath = "projects/" + glabEncodeID(project) + "/runners?per_page=100"
	case owned:
		listPath = "runners?per_page=100"
	}

	var list []gitlabRunnerListEntry
	lres, err := glabAPIJSON(ctx, conn, "GET", listPath, nil, true, &list)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_runner: unable to list runners: " + glabErrMsg(lres)), nil
	}
	var matchID int
	found := false
	for _, r := range list {
		if r.Description == description {
			matchID, found = r.ID, true
			break
		}
	}

	if state == "absent" {
		if !found {
			return Ok(description + " already absent"), nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", "runners/"+strconv.Itoa(matchID), nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_runner: unable to delete " + description + ": " + glabErrMsg(dres)), nil
		}
		return Changed(description + " deleted"), nil
	}

	paused := !argBool(args, "active", true)
	if _, ok := args["paused"]; ok {
		paused = argBool(args, "paused", false)
	}
	locked := argBool(args, "locked", false)
	runUntagged := argBool(args, "run_untagged", true)
	tagList := argStringList(args, "tag_list")
	maxTimeout := argInt(args, "maximum_timeout", 3600)
	accessLevel, hasAccessLevel := args["access_level"]
	_ = accessLevel

	if found {
		var detail gitlabRunnerDetail
		gres, err := glabAPIJSON(ctx, conn, "GET", "runners/"+strconv.Itoa(matchID), nil, false, &detail)
		if err != nil {
			return Result{}, err
		}
		if gres.RC != 0 {
			return Fail("gitlab_runner: unable to read " + description + ": " + glabErrMsg(gres)), nil
		}
		body := map[string]any{}
		if detail.Paused != paused {
			body["paused"] = paused
		}
		if detail.Locked != locked {
			body["locked"] = locked
		}
		if detail.RunUntagged != runUntagged {
			body["run_untagged"] = runUntagged
		}
		if tagList != nil && !stringSetEqual(tagList, detail.TagList) {
			body["tag_list"] = tagList
		}
		if maxTimeout != detail.MaximumTimeout {
			body["maximum_timeout"] = maxTimeout
		}
		if hasAccessLevel {
			want := argString(args, "access_level", "")
			if want != detail.AccessLevel {
				body["access_level"] = want
			}
		}
		if len(body) == 0 {
			return Ok(description+" already up to date").WithExtra("runner", detail), nil
		}
		var updated gitlabRunnerDetail
		ures, err := glabAPIJSON(ctx, conn, "PUT", "runners/"+strconv.Itoa(matchID), body, false, &updated)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return Fail("gitlab_runner: unable to update " + description + ": " + glabErrMsg(ures)), nil
		}
		return Changed(description+" updated").WithExtra("runner", updated), nil
	}

	registrationToken := argString(args, "registration_token", "")
	body := map[string]any{
		"description":  description,
		"paused":       paused,
		"locked":       locked,
		"run_untagged": runUntagged,
		"tag_list":     tagList,
	}
	if maxTimeout != 0 {
		body["maximum_timeout"] = maxTimeout
	}
	if hasAccessLevel && argBool(args, "access_level_on_creation", true) {
		body["access_level"] = argString(args, "access_level", "")
	}

	var apiPath string
	if registrationToken != "" {
		body["token"] = registrationToken
		apiPath = "runners"
	} else {
		apiPath = "user/runners"
		switch {
		case group != "":
			body["runner_type"] = "group_type"
			gid, err := glabResolveGroupID(ctx, conn, group)
			if err != nil {
				return Result{}, err
			}
			body["group_id"] = gid
		case project != "":
			body["runner_type"] = "project_type"
			pid, err := glabResolveProjectID(ctx, conn, project)
			if err != nil {
				return Result{}, err
			}
			body["project_id"] = pid
		default:
			body["runner_type"] = "instance_type"
		}
	}

	var created gitlabRunnerDetail
	cres, err := glabAPIJSON(ctx, conn, "POST", apiPath, body, false, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return Fail("gitlab_runner: unable to register " + description + ": " + glabErrMsg(cres)), nil
	}
	return Changed(description+" registered").WithExtra("runner", created), nil
}
