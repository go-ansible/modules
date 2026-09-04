package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabBranchObj is `glab api projects/:id/repository/branches/:branch`'s
// own JSON object — only the fields this module reads.
type gitlabBranchObj struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Default   bool   `json:"default"`
}

// moduleGitlabBranch implements Ansible's `gitlab_branch`
// (community.general) module: creates or deletes a repository branch,
// via `glab api` against GitLab's own GET/POST/DELETE
// /projects/:id/repository/branches(/:branch) — see gitlab_common.go's
// own doc comment for the `glab` substitution this whole batch makes
// and the api_*/validate_certs/ca_path arguments this module accepts
// but does not act on (the same accepted-but-inert narrowing
// ipa_common.go's own doc comment documents for `ipa`'s connection
// arguments). `glab` has no dedicated branch subcommand — verified
// against glab's own published command reference (`glab repo`/`glab
// mr`/`glab issue`/... list no `branch` family) — so every request here
// goes through `glab api`, exactly like this batch's sibling
// gitlab_milestone.go/gitlab_protected_branch.go.
//
// Args: project (required); branch (required); ref_branch (required
// when state=present, matching real gitlab_branch's own doc: "This must
// be specified if state=present"); state (present|absent, default
// present).
//
// Idempotent on a GET of the named branch: state=present is a no-op if
// branch already exists (regardless of which ref it was originally cut
// from — matching real gitlab_branch's own behavior, which never
// compares an existing branch's ref_branch, it only checks existence);
// state=absent is a no-op if it does not.
//
// Extra["branch"]: the branch object, present whenever state=present
// leaves (or creates) the branch, matching real gitlab_branch's own
// implicit "the branch as it now stands" shape (real gitlab_branch
// itself has no documented return value beyond msg — this port adds
// the object as a strictly-more-useful superset).
func moduleGitlabBranch(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_branch"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	branch, err := requireString(args, "branch")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_branch: state must be one of present, absent, got %q", state)
	}
	itemPath := "projects/" + glabEncodeID(project) + "/repository/branches/" + glabEncodeID(branch)

	var existing gitlabBranchObj
	gres, err := glabAPIJSON(ctx, conn, "GET", itemPath, nil, false, &existing)
	if err != nil {
		return Result{}, err
	}
	found := gres.RC == 0
	if !found && !glabIsNotFound(gres) {
		return Fail("gitlab_branch: unable to read branch " + branch + ": " + glabErrMsg(gres)), nil
	}

	if state == "absent" {
		if !found {
			return Ok(branch + " already absent"), nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", itemPath, nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_branch: unable to delete " + branch + ": " + glabErrMsg(dres)), nil
		}
		return Changed(branch + " deleted"), nil
	}

	if found {
		return Ok(branch+" already exists").WithExtra("branch", existing), nil
	}

	refBranch, err := requireString(args, "ref_branch")
	if err != nil {
		return Result{}, errArg("gitlab_branch: ref_branch is required when state=present and branch does not exist yet: %v", err)
	}
	body := map[string]any{"branch": branch, "ref": refBranch}
	var created gitlabBranchObj
	cres, err := glabAPIJSON(ctx, conn, "POST", "projects/"+glabEncodeID(project)+"/repository/branches", body, false, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return Fail("gitlab_branch: unable to create " + branch + ": " + glabErrMsg(cres)), nil
	}
	return Changed(branch+" created").WithExtra("branch", created), nil
}
