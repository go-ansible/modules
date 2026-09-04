package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabGroupObj is `glab api groups/:id`'s own JSON object — only the
// fields this module reads or writes.
type gitlabGroupObj struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	FullPath string `json:"full_path"`
}

// gitlabGroupFields lists this module's own optional args that map
// 1:1 onto a same-named GitLab group API field (verified against
// GitLab's own Groups API reference, matching each field's real
// gitlab_group option name exactly), split by JSON type so
// moduleGitlabGroup can build/compare them generically. avatar_path is
// deliberately excluded: real gitlab_group's own handling uploads a
// local file's bytes as multipart form data at group-creation time
// only; `glab api` has no multipart form support this port could drive
// without a live glab binary to verify against (see gitlab_common.go's
// own doc comment on that sandbox limit), so this is a documented,
// honest gap — avatar_path is accepted for argument-shape compatibility
// but has no effect.
var (
	gitlabGroupStringFields = []string{
		"description", "visibility", "project_creation_level", "subgroup_creation_level",
		"default_branch", "two_factor_grace_period", "enabled_git_access_protocol", "wiki_access_level",
	}
	gitlabGroupBoolFields = []string{
		"lfs_enabled", "request_access_enabled", "auto_devops_enabled", "membership_lock",
		"share_with_group_lock", "require_two_factor_authentication", "mentions_disabled",
		"prevent_forking_outside_group", "prevent_sharing_groups_outside_hierarchy",
		"service_access_tokens_expiration_enforced", "lock_duo_features_enabled",
	}
)

// moduleGitlabGroup implements Ansible's `gitlab_group`
// (community.general) module: creates, updates, or deletes a GitLab
// group, via `glab api` against GitLab's own GET/POST/PUT/DELETE
// /groups(/:id) — see gitlab_common.go's own doc comment for the
// `glab` substitution and its accepted-but-inert api_*/validate_certs/
// ca_path arguments. `glab` has no dedicated group-management
// subcommand (only `glab repo create --group` for placing a new
// PROJECT under an existing group, a different operation entirely), so
// every request here goes through `glab api`, matching this batch's
// sibling gitlab_milestone.go/gitlab_project_*.go modules.
//
// Args: name (required); path (default: name); parent (ID or full path
// of the parent group — a subgroup's own full_path is `<parent's own
// full_path>/<path>`, resolved via one extra GET when parent is given
// as a numeric ID rather than already a full path); visibility
// (default private); force_delete (bool, default false — state=absent
// only: without it, deleting a group that still has projects fails
// cleanly (Result{Failed:true}), matching real gitlab_group's own
// documented delete_group(force=...) check) — plus every field named in
// gitlabGroupStringFields/gitlabGroupBoolFields, all optional and left
// untouched on the group when omitted (this module only ever sends the
// fields a task actually specifies); avatar_path (accepted, NOT wired —
// see gitlabGroupStringFields' own doc comment); state (present|absent,
// default present).
//
// Idempotent: state=present with no existing group at the resolved
// full_path creates one (POST /groups) with every specified field.
// With an existing group, compares each specified field's desired
// value against the freshly-read current group object (via
// jsonScalarEqual, shared with gitlab_project_approvals.go) and PUTs
// only when at least one differs — path itself is deliberately NOT
// compared/updated after creation (changing a group's path/full_path
// mid-run would invalidate this module's own subsequent full_path-based
// lookup within the same task list; a documented simplification, not
// an oversight). state=absent: not found is a no-op; found with
// force_delete=false and at least one project inside fails cleanly (an
// expected, well-formed refusal, not an infrastructure error); found
// otherwise is deleted.
//
// Extra["group"]: the group object as it now stands, present whenever
// state=present, matching real gitlab_group's own documented return.
func moduleGitlabGroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_group"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	path := argString(args, "path", name)
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_group: state must be one of present, absent, got %q", state)
	}

	fullPath := path
	var parentID int
	if parent := argString(args, "parent", ""); parent != "" {
		var parentObj gitlabGroupObj
		pres, err := glabAPIJSON(ctx, conn, "GET", "groups/"+glabEncodeID(parent), nil, false, &parentObj)
		if err != nil {
			return Result{}, err
		}
		if pres.RC != 0 {
			return Fail("gitlab_group: unable to resolve parent group " + parent + ": " + glabErrMsg(pres)), nil
		}
		parentID = parentObj.ID
		fullPath = parentObj.FullPath + "/" + path
	}

	var existing gitlabGroupObj
	gres, err := glabAPIJSON(ctx, conn, "GET", "groups/"+glabEncodeID(fullPath), nil, false, &existing)
	if err != nil {
		return Result{}, err
	}
	found := gres.RC == 0
	if !found && !glabIsNotFound(gres) {
		return Fail("gitlab_group: unable to read group " + fullPath + ": " + glabErrMsg(gres)), nil
	}

	if state == "absent" {
		if !found {
			return Ok(fullPath + " already absent"), nil
		}
		if !argBool(args, "force_delete", false) {
			var projects []map[string]any
			plres, err := glabAPIJSON(ctx, conn, "GET", "groups/"+glabEncodeID(fullPath)+"/projects?per_page=1", nil, false, &projects)
			if err != nil {
				return Result{}, err
			}
			if plres.RC == 0 && len(projects) > 0 {
				return Fail("gitlab_group: there are still projects in this group; these need to be moved or " +
					"deleted before this group can be removed, or set force_delete=true"), nil
			}
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", "groups/"+glabEncodeID(fullPath), nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_group: unable to delete " + fullPath + ": " + glabErrMsg(dres)), nil
		}
		return Changed(fullPath + " deleted"), nil
	}

	if !found {
		body := map[string]any{"name": name, "path": path, "visibility": argString(args, "visibility", "private")}
		if parentID != 0 {
			body["parent_id"] = parentID
		}
		gitlabGroupApplyFields(args, body)
		var created gitlabGroupObj
		cres, err := glabAPIJSON(ctx, conn, "POST", "groups", body, false, &created)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("gitlab_group: unable to create " + fullPath + ": " + glabErrMsg(cres)), nil
		}
		return Changed(fullPath+" created").WithExtra("group", created), nil
	}

	var currentRaw map[string]any
	rres, err := glabAPIJSON(ctx, conn, "GET", "groups/"+glabEncodeID(fullPath), nil, false, &currentRaw)
	if err != nil {
		return Result{}, err
	}
	if rres.RC != 0 {
		return Fail("gitlab_group: unable to re-read " + fullPath + ": " + glabErrMsg(rres)), nil
	}

	body := map[string]any{}
	if _, ok := args["visibility"]; ok {
		want := argString(args, "visibility", "")
		if have, existed := currentRaw["visibility"]; !existed || !jsonScalarEqual(want, have) {
			body["visibility"] = want
		}
	}
	gitlabGroupDiffFields(args, currentRaw, body)

	if len(body) == 0 {
		return Ok(fullPath+" already up to date").WithExtra("group", existing), nil
	}
	var updated gitlabGroupObj
	ures, err := glabAPIJSON(ctx, conn, "PUT", "groups/"+glabEncodeID(fullPath), body, false, &updated)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return Fail("gitlab_group: unable to update " + fullPath + ": " + glabErrMsg(ures)), nil
	}
	return Changed(fullPath+" updated").WithExtra("group", updated), nil
}

// gitlabGroupApplyFields copies every specified string/bool field named
// in gitlabGroupStringFields/gitlabGroupBoolFields from args into body,
// used at creation time (every specified field is sent, nothing to
// compare against yet).
func gitlabGroupApplyFields(args map[string]any, body map[string]any) {
	for _, f := range gitlabGroupStringFields {
		if _, ok := args[f]; ok {
			body[f] = argString(args, f, "")
		}
	}
	for _, f := range gitlabGroupBoolFields {
		if _, ok := args[f]; ok {
			body[f] = argBool(args, f, false)
		}
	}
}

// gitlabGroupDiffFields is gitlabGroupApplyFields' update-time
// counterpart: a field is only added to body when its desired value
// differs from current's own same-named field (via jsonScalarEqual,
// shared with gitlab_project_approvals.go).
func gitlabGroupDiffFields(args map[string]any, current map[string]any, body map[string]any) {
	for _, f := range gitlabGroupStringFields {
		if _, ok := args[f]; !ok {
			continue
		}
		want := argString(args, f, "")
		if have, existed := current[f]; !existed || !jsonScalarEqual(want, have) {
			body[f] = want
		}
	}
	for _, f := range gitlabGroupBoolFields {
		if _, ok := args[f]; !ok {
			continue
		}
		want := argBool(args, f, false)
		if have, existed := current[f]; !existed || !jsonScalarEqual(want, have) {
			body[f] = want
		}
	}
}
