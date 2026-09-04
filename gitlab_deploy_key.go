package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabDeployKeyObj is one entry of `glab deploy-key list -F json`'s
// own JSON array (a direct passthrough of GitLab's own GET
// /projects/:id/deploy_keys response) — only the fields this module
// reads.
type gitlabDeployKeyObj struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Key     string `json:"key"`
	CanPush bool   `json:"can_push"`
}

// moduleGitlabDeployKey implements Ansible's `gitlab_deploy_key`
// (community.general) module: adds, updates, or removes a project
// deploy key.
//
// Unlike most of this batch's gitlab_* modules, `glab` DOES have a
// dedicated subcommand family for this resource — `glab deploy-key
// add`/`list`/`delete`/`get` (verified against docs.gitlab.com/cli/
// deploy-key/*, per this batch's own instruction to check before
// assuming `glab api` is the only option) — so this module drives those
// directly (via gitlab_dedicated_common.go's own glabCLI, shared with
// gitlab_issue.go/gitlab_merge_request.go) rather than `glab api`. The
// ONE gap: `glab deploy-key` has no `update` subcommand at all, so a
// can_push-only change (key title and content both unchanged) falls
// back to `glab api PUT .../deploy_keys/:id` — the same
// "prefer-dedicated, fall back to `glab api` for what it can't do"
// pattern this batch's own instructions describe, and see
// gitlab_common.go's own doc comment for the accepted-but-inert
// api_*/validate_certs/ca_path arguments this module (like every other
// one in this batch) does not wire in.
//
// Args: project (required); title (required) — this port's (and real
// gitlab_deploy_key's own, via its find_deploy_key) matching key, since
// deploy keys have no other natural key; key (required, even for
// state=absent — matching real gitlab_deploy_key's own argument_spec
// exactly, even though the absent path below never actually reads it,
// since real gitlab_deploy_key's own absent path matches by title
// alone too); can_push (bool, default false); state (present|absent,
// default present).
//
// state=absent: a deploy key with matching title is deleted (`glab
// deploy-key delete`); no match is a no-op. state=present: no match ->
// `glab deploy-key add` (key content piped over stdin via `-`, not put
// on argv — see glabCLI's own doc comment). A match whose key content
// differs is deleted then re-added (GitLab's own REST API cannot update
// a deploy key's public key content in place — matching real
// gitlab_deploy_key's own documented comment: "public key cannot be
// updated directly by GitLab REST API, so ... delete and than recreate
// the key"). A match with the same key content but a different
// can_push is updated via the `glab api` PUT fallback described above.
// Otherwise a no-op.
//
// Extra["deploy_key"]: the resulting deploy key object, present
// whenever state=present, matching real gitlab_deploy_key's own
// documented return.
func moduleGitlabDeployKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_deploy_key"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	title, err := requireString(args, "title")
	if err != nil {
		return Result{}, err
	}
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	canPush := argBool(args, "can_push", false)
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_deploy_key: state must be one of present, absent, got %q", state)
	}

	lres, err := glabCLI(ctx, conn, nil, "glab", "deploy-key", "list", "-R", project, "-F", "json", "--per-page", "100")
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_deploy_key: unable to list deploy keys: " + strings.TrimSpace(lres.Stderr)), nil
	}
	var keys []gitlabDeployKeyObj
	if strings.TrimSpace(lres.Stdout) != "" {
		if err := json.Unmarshal([]byte(lres.Stdout), &keys); err != nil {
			return Result{}, err
		}
	}
	var existing *gitlabDeployKeyObj
	for i := range keys {
		if keys[i].Title == title {
			existing = &keys[i]
			break
		}
	}

	if state == "absent" {
		if existing == nil {
			return Ok(title + " already absent"), nil
		}
		dres, err := glabCLI(ctx, conn, nil, "glab", "deploy-key", "delete", strconv.Itoa(existing.ID), "-R", project)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_deploy_key: unable to delete " + title + ": " + strings.TrimSpace(dres.Stderr)), nil
		}
		return Changed(title + " deleted"), nil
	}

	addKey := func() (Result, error) {
		argv := []string{"glab", "deploy-key", "add", "-R", project, "-t", title}
		if canPush {
			argv = append(argv, "--can-push")
		}
		argv = append(argv, "-")
		ares, err := glabCLI(ctx, conn, strings.NewReader(key+"\n"), argv...)
		if err != nil {
			return Result{}, err
		}
		if ares.RC != 0 {
			return Fail("gitlab_deploy_key: unable to create " + title + ": " + strings.TrimSpace(ares.Stderr)), nil
		}
		lres2, err := glabCLI(ctx, conn, nil, "glab", "deploy-key", "list", "-R", project, "-F", "json", "--per-page", "100")
		if err != nil {
			return Result{}, err
		}
		var created gitlabDeployKeyObj
		if lres2.RC == 0 {
			var keys2 []gitlabDeployKeyObj
			if json.Unmarshal([]byte(lres2.Stdout), &keys2) == nil {
				for _, k := range keys2 {
					if k.Title == title {
						created = k
						break
					}
				}
			}
		}
		return Changed(title+" created").WithExtra("deploy_key", created), nil
	}

	if existing == nil {
		return addKey()
	}

	if strings.TrimSpace(existing.Key) != strings.TrimSpace(key) {
		dres, err := glabCLI(ctx, conn, nil, "glab", "deploy-key", "delete", strconv.Itoa(existing.ID), "-R", project)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_deploy_key: unable to remove previous " + title + " before recreating: " + strings.TrimSpace(dres.Stderr)), nil
		}
		return addKey()
	}

	if existing.CanPush != canPush {
		body := map[string]any{"can_push": canPush}
		var updated gitlabDeployKeyObj
		ures, err := glabAPIJSON(ctx, conn, "PUT", "projects/"+glabEncodeID(project)+"/deploy_keys/"+strconv.Itoa(existing.ID), body, false, &updated)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return Fail("gitlab_deploy_key: unable to update can_push for " + title + ": " + glabErrMsg(ures)), nil
		}
		return Changed(title+" updated").WithExtra("deploy_key", updated), nil
	}

	return Ok(title+" already up to date").WithExtra("deploy_key", *existing), nil
}
