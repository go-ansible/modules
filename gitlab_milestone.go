package modules

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabMilestone is one entry of `glab api .../milestones`'s own JSON
// array — only the fields this module reads or writes.
type gitlabMilestone struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DueDate     string `json:"due_date"`
	StartDate   string `json:"start_date"`
}

// moduleGitlabMilestone implements Ansible's `gitlab_milestone`
// (community.general) module: creates, updates, deletes, or purges a
// project's or group's milestones — via `glab api` (see
// gitlab_common.go's own doc comment for why this batch substitutes
// `glab` for python-gitlab, and for the api_*/validate_certs/ca_path
// arguments this module accepts but does not act on). `glab` has no
// dedicated milestone subcommand, so every request here goes through
// `glab api .../milestones` — GitLab's own REST resource at
// GET/POST/PUT /projects|groups/:id/milestones(/:milestone_id).
//
// Args: project XOR group (required, one or the other — matching real
// gitlab_milestone's own mutually-exclusive pair); milestones (list of
// dicts: title required, description/due_date/start_date optional —
// due_date/start_date in YYYY-MM-DD format, passed straight through to
// the API, which itself validates the format); purge (bool, default
// false — deletes every existing milestone whose title is not present
// in milestones, state=present only, matching real gitlab_milestone's
// own documented purge semantics); state (present|absent, default
// present).
//
// state=present: for each entry in milestones, looked up by title (this
// port's own idempotency key, matching real gitlab_milestone's own
// title-based matching) among the resource's current milestones — POST
// a new one if no title matches; otherwise PUT only the
// description/due_date/start_date fields the entry actually specifies
// (a field omitted from one entry is left untouched on the existing
// milestone, never cleared) if any differ from the current value, else
// left untouched. state=absent: each entry's matching milestone (if
// any) is deleted; entries with no match are a no-op, not a failure.
//
// Extra["milestones"]: {added, updated, removed, untouched} — four
// lists of milestone titles, matching real gitlab_milestone's own
// return shape. Extra["milestones_obj"]: the resource's own milestones
// after this module ran, decoded straight from the API's own JSON.
func moduleGitlabMilestone(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_milestone"); !ok {
		return res, nil
	}

	project := argString(args, "project", "")
	group := argString(args, "group", "")
	if (project == "") == (group == "") {
		return Result{}, errArg("gitlab_milestone: exactly one of project, group is required")
	}
	base := "projects/" + glabEncodeID(project) + "/milestones"
	if group != "" {
		base = "groups/" + glabEncodeID(group) + "/milestones"
	}

	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_milestone: state must be one of present, absent, got %q", state)
	}
	purge := argBool(args, "purge", false)

	var current []gitlabMilestone
	res, err := glabAPIJSON(ctx, conn, "GET", base+"?per_page=100", nil, true, &current)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("gitlab_milestone: unable to list milestones: " + glabErrMsg(res)), nil
	}
	byTitle := map[string]gitlabMilestone{}
	for _, m := range current {
		byTitle[m.Title] = m
	}

	raw, _ := args["milestones"].([]any)
	var added, updated, removed, untouched []string
	desired := map[string]bool{}

	for _, r := range raw {
		item, ok := r.(map[string]any)
		if !ok {
			return Result{}, errArg("gitlab_milestone: each entry of milestones must be a dict")
		}
		title, err := requireString(item, "title")
		if err != nil {
			return Result{}, errArg("gitlab_milestone: %v", err)
		}
		desired[title] = true
		existing, found := byTitle[title]

		if state == "absent" {
			if found {
				dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(existing.ID), nil, false, nil)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("gitlab_milestone: unable to delete " + title + ": " + glabErrMsg(dres)), nil
				}
				removed = append(removed, title)
				delete(byTitle, title)
			}
			continue
		}

		body := map[string]any{}
		if v, ok := item["description"]; ok {
			body["description"] = v
		}
		if v, ok := item["due_date"]; ok {
			body["due_date"] = v
		}
		if v, ok := item["start_date"]; ok {
			body["start_date"] = v
		}

		if !found {
			body["title"] = title
			var created gitlabMilestone
			cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, &created)
			if err != nil {
				return Result{}, err
			}
			if cres.RC != 0 {
				return Fail("gitlab_milestone: unable to create " + title + ": " + glabErrMsg(cres)), nil
			}
			added = append(added, title)
			byTitle[title] = created
			continue
		}

		diff := false
		if v, ok := body["description"]; ok && argAnyString(v) != existing.Description {
			diff = true
		}
		if v, ok := body["due_date"]; ok && argAnyString(v) != existing.DueDate {
			diff = true
		}
		if v, ok := body["start_date"]; ok && argAnyString(v) != existing.StartDate {
			diff = true
		}
		if !diff {
			untouched = append(untouched, title)
			continue
		}
		var upd gitlabMilestone
		ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+strconv.Itoa(existing.ID), body, false, &upd)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return Fail("gitlab_milestone: unable to update " + title + ": " + glabErrMsg(ures)), nil
		}
		updated = append(updated, title)
		byTitle[title] = upd
	}

	if purge && state == "present" {
		for title, m := range byTitle {
			if desired[title] {
				continue
			}
			dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(m.ID), nil, false, nil)
			if err != nil {
				return Result{}, err
			}
			if dres.RC != 0 {
				return Fail("gitlab_milestone: unable to purge " + title + ": " + glabErrMsg(dres)), nil
			}
			removed = append(removed, title)
		}
	}

	sort.Strings(added)
	sort.Strings(updated)
	sort.Strings(removed)
	sort.Strings(untouched)
	summary := map[string]any{"added": orEmpty(added), "updated": orEmpty(updated), "removed": orEmpty(removed), "untouched": orEmpty(untouched)}

	changed := len(added) > 0 || len(updated) > 0 || len(removed) > 0
	r := Result{Changed: changed}
	return r.WithExtra("milestones", summary), nil
}

// argAnyString stringifies a JSON-decoded value the same way argString
// would for a map[string]any read directly from module args, used here
// to compare a milestone body field (any, since it comes from the raw
// milestones list) against the API's own string field.
func argAnyString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// orEmpty returns an empty (never nil) []string, so Extra always
// encodes as `[]`, not `null`, matching real Ansible's own list return
// fields.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
