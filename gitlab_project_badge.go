package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabBadgeObj is one entry of `glab api projects/:id/badges`'s own
// JSON array/object.
type gitlabBadgeObj struct {
	ID       int    `json:"id"`
	LinkURL  string `json:"link_url"`
	ImageURL string `json:"image_url"`
	Kind     string `json:"kind"`
}

// moduleGitlabProjectBadge implements Ansible's `gitlab_project_badge`
// (community.general) module: adds or removes a project badge, via
// `glab api` against GitLab's own GET/POST/PUT/DELETE
// /projects/:id/badges(/:id) — see gitlab_common.go's own doc comment
// for the `glab` substitution and its accepted-but-inert
// api_*/validate_certs/ca_path arguments. `glab` has no dedicated
// subcommand for this resource.
//
// Args: project (required); image_url (required) — this port's (and
// real gitlab_project_badge's own, per its own doc: "A badge is
// identified by this URL") matching key, since a project's badges have
// no other natural key; link_url (required); state (present|absent,
// default present).
//
// state=present: creates the badge (POST) if no existing badge has the
// same image_url; if one does and its link_url differs, updates it
// (PUT); otherwise a no-op. state=absent: deletes the matching badge
// (DELETE) if one exists; a no-op otherwise.
//
// Extra["badge"]: the badge object, present when state=present (whether
// newly created, updated, or already matching), matching real
// gitlab_project_badge's own documented return.
func moduleGitlabProjectBadge(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_project_badge"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	imageURL, err := requireString(args, "image_url")
	if err != nil {
		return Result{}, err
	}
	linkURL := argString(args, "link_url", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_project_badge: state must be one of present, absent, got %q", state)
	}
	base := "projects/" + glabEncodeID(project) + "/badges"

	var badges []gitlabBadgeObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base+"?per_page=100", nil, true, &badges)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_project_badge: unable to list badges: " + glabErrMsg(lres)), nil
	}
	var existing *gitlabBadgeObj
	for i := range badges {
		if badges[i].ImageURL == imageURL {
			existing = &badges[i]
			break
		}
	}

	if state == "absent" {
		if existing == nil {
			return Ok("badge already absent"), nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(existing.ID), nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_project_badge: unable to delete badge: " + glabErrMsg(dres)), nil
		}
		return Changed("badge deleted"), nil
	}

	if existing == nil {
		body := map[string]any{"link_url": linkURL, "image_url": imageURL}
		var created gitlabBadgeObj
		cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, &created)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("gitlab_project_badge: unable to create badge: " + glabErrMsg(cres)), nil
		}
		return Changed("badge created").WithExtra("badge", created), nil
	}

	if existing.LinkURL == linkURL {
		return Ok("badge already up to date").WithExtra("badge", *existing), nil
	}
	body := map[string]any{"link_url": linkURL, "image_url": imageURL}
	var updated gitlabBadgeObj
	ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+strconv.Itoa(existing.ID), body, false, &updated)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return Fail("gitlab_project_badge: unable to update badge: " + glabErrMsg(ures)), nil
	}
	return Changed("badge updated").WithExtra("badge", updated), nil
}
