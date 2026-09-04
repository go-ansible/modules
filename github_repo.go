package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGithubRepo implements Ansible's `github_repo`
// (community.general) module: creates, deletes, or updates the
// description/visibility of a GitHub repository, via `gh repo create`/
// `delete`/`edit`/`view` — see github_common.go's own doc comment for
// why this port substitutes the `gh` CLI for real github_repo's own
// PyGithub-based REST API calls.
//
// Args: name (required) — repository name; organization (optional) —
// when unset, the repository is created/looked up under the
// currently-authenticated `gh` user (matching real github_repo's own
// `gh.get_user()` fallback), not a module argument-supplied username;
// state (present|absent, default present); private (bool); description
// (string); force_defaults (bool, default false) — when true, an unset
// `private`/`description` defaults to false/"" instead of being left
// alone; access_token — wired into GH_TOKEN (see github_common.go);
// username/password — accepted, no effect (see github_common.go; real
// github_repo's own PyGithub basic-auth login has no `gh` equivalent).
//
// Deviation — api_url is accepted for argument-shape compatibility but
// has no effect, same reasoning as github_deploy_key's github_url (see
// that module's own doc comment).
//
// state=absent: `gh repo view <spec>` first (spec is `organization/name`
// or just `name` under the current user); if found, `gh repo delete
// <spec> --yes` (Changed=true), else Changed=false — matching real
// delete_repo's own UnknownObjectException-is-a-no-op handling.
//
// state=present: `gh repo view` first; if not found, `gh repo create
// <spec> --public|--private [--description desc]` (gh's own
// non-interactive requirement: exactly one of --public/--private/
// --internal must be given — this port always passes one, computed
// from `private`, defaulting to --public matching real create_repo's
// own `GithubObject.NotSet if private is None else private` combined
// with PyGithub's own server-side default of private=false),
// Changed=true. Then, whether freshly created or pre-existing, this
// port compares the CURRENT repo's own isPrivate/description (from
// `gh repo view --json`) against the requested private/description (if
// given) and issues `gh repo edit` only for an actual difference —
// `--visibility public|private --accept-visibility-change-consequences`
// and/or `--description desc` — Changed=true if anything was edited,
// matching real create_repo's own changes-dict diff-then-edit
// structure. force_defaults folds unset private/description to
// false/"" BEFORE any of this, matching real run_module's own
// `params["description"] = params["description"] or ""` (etc.)
// pre-processing exactly.
//
// Extra["repo"] (state=present only) is `gh repo view`'s own JSON
// object, fields: name, owner (nested login), description, isPrivate,
// visibility, url, sshUrl, createdAt, updatedAt, defaultBranchRef.
//
// Deviation — repo JSON shape: real github_repo's own Extra["repo"] is
// PyGithub's raw_data — GitHub's REST API's own snake_case repository
// resource (private, full_name, clone_url, ...) verbatim. `gh repo
// view --json`'s own field set uses different names and camelCase
// (isPrivate not private, sshUrl not ssh_url, no full_name/clone_url
// at all) and is not a 1:1 re-shaping of the REST resource — this port
// does not attempt to translate one into the other's exact key names;
// a caller relying on Extra["repo"]'s specific keys needs this port's
// own shape, not real github_repo's.
func moduleGithubRepo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("github_repo: state must be one of present, absent, got %q", state)
	}
	org := argString(args, "organization", "")
	token := argString(args, "access_token", "")
	forceDefaults := argBool(args, "force_defaults", false)

	spec := name
	if org != "" {
		spec = org + "/" + name
	}

	var private *bool
	if _, ok := args["private"]; ok {
		v := argBool(args, "private", false)
		private = &v
	}
	var description *string
	if _, ok := args["description"]; ok {
		v := argString(args, "description", "")
		description = &v
	}
	if forceDefaults {
		if private == nil {
			v := false
			private = &v
		}
		if description == nil {
			v := ""
			description = &v
		}
	}

	if state == "absent" {
		viewRes, err := ghRun(ctx, conn, token, nil, "repo", "view", spec, "--json", "name")
		if err != nil {
			return Result{}, err
		}
		if viewRes.RC != 0 {
			return Ok(""), nil
		}
		delRes, err := ghRun(ctx, conn, token, nil, "repo", "delete", spec, "--yes")
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("github_repo: failed to delete repository " + spec + ": " + ghStderr(delRes)), nil
		}
		return Changed(""), nil
	}

	changed := false
	current, exists, err := ghRepoView(ctx, conn, token, spec)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		createArgs := []string{"repo", "create", spec}
		if private != nil && *private {
			createArgs = append(createArgs, "--private")
		} else {
			createArgs = append(createArgs, "--public")
		}
		if description != nil && *description != "" {
			createArgs = append(createArgs, "--description", *description)
		}
		createRes, err := ghRun(ctx, conn, token, nil, createArgs...)
		if err != nil {
			return Result{}, err
		}
		if createRes.RC != 0 {
			return Fail("github_repo: failed to create repository " + spec + ": " + ghStderr(createRes)), nil
		}
		changed = true
		current, _, err = ghRepoView(ctx, conn, token, spec)
		if err != nil {
			return Result{}, err
		}
	}

	var editArgs []string
	if private != nil && *private != current.IsPrivate {
		visibility := "public"
		if *private {
			visibility = "private"
		}
		editArgs = append(editArgs, "--visibility", visibility, "--accept-visibility-change-consequences")
	}
	if description != nil && *description != current.Description {
		editArgs = append(editArgs, "--description", *description)
	}
	if len(editArgs) > 0 {
		editRes, err := ghRun(ctx, conn, token, nil, append([]string{"repo", "edit", spec}, editArgs...)...)
		if err != nil {
			return Result{}, err
		}
		if editRes.RC != 0 {
			return Fail("github_repo: failed to edit repository " + spec + ": " + ghStderr(editRes)), nil
		}
		changed = true
		current, _, err = ghRepoView(ctx, conn, token, spec)
		if err != nil {
			return Result{}, err
		}
	}

	res := Result{Changed: changed}
	return res.WithExtra("repo", current.raw), nil
}

type ghRepoInfo struct {
	IsPrivate   bool
	Description string
	raw         map[string]any
}

// ghRepoView runs `gh repo view <spec> --json ...` and decodes it both
// into the typed fields moduleGithubRepo's own diff logic needs and a
// raw map[string]any for Extra["repo"] — see moduleGithubRepo's own
// doc comment on why this port's repo JSON shape differs from real
// github_repo's own PyGithub raw_data.
func ghRepoView(ctx context.Context, conn remoteexec.Connection, token, spec string) (ghRepoInfo, bool, error) {
	fields := "name,owner,description,isPrivate,visibility,url,sshUrl,createdAt,updatedAt,defaultBranchRef"
	var raw map[string]any
	res, err := ghRunJSON(ctx, conn, token, &raw, "repo", "view", spec, "--json", fields)
	if err != nil {
		return ghRepoInfo{}, false, err
	}
	if res.RC != 0 {
		return ghRepoInfo{}, false, nil
	}
	info := ghRepoInfo{raw: raw}
	if b, ok := raw["isPrivate"].(bool); ok {
		info.IsPrivate = b
	}
	if s, ok := raw["description"].(string); ok {
		info.Description = s
	}
	return info, true, nil
}
