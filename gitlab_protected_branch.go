package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabProtectedBranchObj is `glab api
// projects/:id/protected_branches/:name`'s own JSON object — only the
// fields this module reads.
type gitlabProtectedBranchObj struct {
	Name             string `json:"name"`
	PushAccessLevels []struct {
		AccessLevel int `json:"access_level"`
	} `json:"push_access_levels"`
	MergeAccessLevels []struct {
		AccessLevel int `json:"access_level"`
	} `json:"merge_access_levels"`
	AllowForcePush            bool `json:"allow_force_push"`
	CodeOwnerApprovalRequired bool `json:"code_owner_approval_required"`
}

// moduleGitlabProtectedBranch implements Ansible's
// `gitlab_protected_branch` (community.general) module: protects or
// unprotects a branch (or wildcard branch pattern), via `glab api`
// against GitLab's own GET/POST/DELETE
// /projects/:id/protected_branches(/:name) — see gitlab_common.go's own
// doc comment for the `glab` substitution and its accepted-but-inert
// api_*/validate_certs/ca_path arguments. `glab` has no dedicated
// subcommand for this resource.
//
// Args: project (required); name (required) — the branch name or
// wildcard pattern; push_access_level/merge_access_levels (maintainer|
// developer|nobody, both default maintainer); allow_force_push (bool);
// code_owner_approval_required (bool); state (present|absent, default
// present).
//
// state=absent: an existing protection on name is removed (DELETE); a
// no-op otherwise. state=present: if name is not currently protected,
// POST protects it with the requested settings. If it IS already
// protected with a matching push/merge access level and
// allow_force_push/code_owner_approval_required, this is a no-op.
// Deviation: GitLab's REST API has no single "update a protected
// branch's access levels" call this port could verify works across
// every GitLab version this batch targets (a PATCH endpoint for
// allow_force_push/code_owner_approval_required alone was added in a
// later GitLab release, but changing the push/merge access level
// itself has never had an update verb) — so, matching what real
// gitlab_protected_branch's own python-gitlab-backed implementation
// does for the same case (unprotect, then re-protect with the new
// settings), a settings change here always issues DELETE followed by
// POST rather than attempting an in-place PATCH.
func moduleGitlabProtectedBranch(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_protected_branch"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_protected_branch: state must be one of present, absent, got %q", state)
	}
	base := "projects/" + glabEncodeID(project) + "/protected_branches"
	itemPath := base + "/" + glabEncodeID(name)

	var existing gitlabProtectedBranchObj
	gres, err := glabAPIJSON(ctx, conn, "GET", itemPath, nil, false, &existing)
	if err != nil {
		return Result{}, err
	}
	found := gres.RC == 0
	if !found && !glabIsNotFound(gres) {
		return Fail("gitlab_protected_branch: unable to read protection for " + name + ": " + glabErrMsg(gres)), nil
	}

	if state == "absent" {
		if !found {
			return Ok(name + " already unprotected"), nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", itemPath, nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_protected_branch: unable to unprotect " + name + ": " + glabErrMsg(dres)), nil
		}
		return Changed(name + " unprotected"), nil
	}

	pushLevelName := argString(args, "push_access_level", "maintainer")
	mergeLevelName := argString(args, "merge_access_levels", "maintainer")
	pushLevel, err := glabAccessLevel(pushLevelName)
	if err != nil {
		return Result{}, errArg("gitlab_protected_branch: push_access_level: %v", err)
	}
	mergeLevel, err := glabAccessLevel(mergeLevelName)
	if err != nil {
		return Result{}, errArg("gitlab_protected_branch: merge_access_levels: %v", err)
	}
	allowForcePush := argBool(args, "allow_force_push", false)
	codeOwnerApproval := argBool(args, "code_owner_approval_required", false)

	if found {
		samePush := len(existing.PushAccessLevels) > 0 && existing.PushAccessLevels[0].AccessLevel == pushLevel
		sameMerge := len(existing.MergeAccessLevels) > 0 && existing.MergeAccessLevels[0].AccessLevel == mergeLevel
		if samePush && sameMerge && existing.AllowForcePush == allowForcePush && existing.CodeOwnerApprovalRequired == codeOwnerApproval {
			return Ok(name + " already protected as requested"), nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", itemPath, nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_protected_branch: unable to unprotect " + name + " before re-protecting: " + glabErrMsg(dres)), nil
		}
	}

	body := map[string]any{
		"name":                         name,
		"push_access_level":            pushLevel,
		"merge_access_level":           mergeLevel,
		"allow_force_push":             allowForcePush,
		"code_owner_approval_required": codeOwnerApproval,
	}
	cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, nil)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return Fail("gitlab_protected_branch: unable to protect " + name + ": " + glabErrMsg(cres)), nil
	}
	return Changed(name + " protected"), nil
}
