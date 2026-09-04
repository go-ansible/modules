package modules

import (
	"context"
	"sort"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabLabelObj is one entry of `glab api
// projects|groups/:id/labels`'s own JSON array — only the fields this
// module reads or writes.
type gitlabLabelObj struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
}

// moduleGitlabLabel implements Ansible's `gitlab_label`
// (community.general) module: creates, updates, deletes, or purges a
// project's or group's labels, via `glab api` against GitLab's own
// GET/POST/PUT/DELETE /projects|groups/:id/labels(/:name) — see
// gitlab_common.go's own doc comment for the `glab` substitution and
// its accepted-but-inert api_*/validate_certs/ca_path arguments.
//
// `glab label create`/`list`/`delete` exist, but their own group-scope
// support is inconsistent: `list` has a documented `-g/--group` flag,
// while `create` and `delete` do NOT (verified against
// docs.gitlab.com/cli/label/{create,list,delete}/ — `create`'s page
// lists only -c/-d/-n/-p/-R, `delete`'s only -R, neither has -g/
// --group, per this batch's own instruction to check before assuming).
// Rather than use the dedicated subcommand for project labels and
// `glab api` for group labels (two different mechanisms for what is
// the same underlying resource, differing only in scope), this module
// uses `glab api` uniformly for both — the same reasoning
// gitlab_group_variable.go's own doc comment gives for the same kind of
// split-support gap on a different resource.
//
// Args: project XOR group (required, one or the other — matching real
// gitlab_label's own mutually-exclusive pair); labels (list of dicts:
// name required; color required for a NEW label; description;
// new_name — renames an existing label matched by name, a no-op if no
// label with that name currently exists, matching real gitlab_label's
// own find-by-name-then-rename behavior; priority (int)); purge (bool,
// default false) — deletes every existing label whose name is not
// named (by name OR new_name) in labels, applies regardless of state
// per real gitlab_label's own EXAMPLES (a purge-only task with no
// labels list and no explicit state); state (present|absent, default
// present).
//
// state=present: for each entry, looked up by its CURRENT name (this
// port's own idempotency key, matching real gitlab_label's own
// name-based matching) — POST a new one if no name matches (color
// required); otherwise PUT only the color/description/priority/
// new_name fields that actually differ from the current label, else
// left untouched. state=absent: each entry's matching label (if any) is
// deleted; no match is a no-op, not a failure.
//
// Extra["labels"]: {added, updated, removed, untouched} — four lists of
// label names, matching real gitlab_label's own documented return
// shape. Extra["labels_obj"]: the resource's own labels after this
// module ran, decoded straight from a fresh GET, matching real
// gitlab_label's own documented return.
func moduleGitlabLabel(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_label"); !ok {
		return res, nil
	}
	project := argString(args, "project", "")
	group := argString(args, "group", "")
	if (project == "") == (group == "") {
		return Result{}, errArg("gitlab_label: exactly one of project, group is required")
	}
	base := "projects/" + glabEncodeID(project) + "/labels"
	if group != "" {
		base = "groups/" + glabEncodeID(group) + "/labels"
	}

	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_label: state must be one of present, absent, got %q", state)
	}
	purge := argBool(args, "purge", false)

	var current []gitlabLabelObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base+"?per_page=100", nil, true, &current)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_label: unable to list labels: " + glabErrMsg(lres)), nil
	}
	byName := map[string]gitlabLabelObj{}
	for _, l := range current {
		byName[l.Name] = l
	}

	raw, _ := args["labels"].([]any)
	var added, updated, removed, untouched []string
	keep := map[string]bool{}

	for _, r := range raw {
		item, ok := r.(map[string]any)
		if !ok {
			return Result{}, errArg("gitlab_label: each entry of labels must be a dict")
		}
		name, err := requireString(item, "name")
		if err != nil {
			return Result{}, errArg("gitlab_label: %v", err)
		}
		newName := argString(item, "new_name", "")
		keep[name] = true
		if newName != "" {
			keep[newName] = true
		}
		existing, found := byName[name]

		if state == "absent" {
			if found {
				dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+glabEncodeID(name), nil, false, nil)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("gitlab_label: unable to delete " + name + ": " + glabErrMsg(dres)), nil
				}
				removed = append(removed, name)
				delete(byName, name)
			} else {
				untouched = append(untouched, name)
			}
			continue
		}

		if !found {
			if newName != "" {
				// Nothing to rename — matching real gitlab_label's own
				// find-by-name-then-rename behavior, documented above.
				untouched = append(untouched, name)
				continue
			}
			color, err := requireString(item, "color")
			if err != nil {
				return Result{}, errArg("gitlab_label: color is required to create %q: %v", name, err)
			}
			body := map[string]any{"name": name, "color": color}
			if v, ok := item["description"]; ok {
				body["description"] = v
			}
			if v, ok := item["priority"]; ok {
				body["priority"] = v
			}
			var created gitlabLabelObj
			cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, &created)
			if err != nil {
				return Result{}, err
			}
			if cres.RC != 0 {
				return Fail("gitlab_label: unable to create " + name + ": " + glabErrMsg(cres)), nil
			}
			added = append(added, name)
			byName[name] = created
			continue
		}

		body := map[string]any{}
		if newName != "" && newName != existing.Name {
			body["new_name"] = newName
		}
		if v, ok := item["color"]; ok && argAnyString(v) != existing.Color {
			body["color"] = v
		}
		if v, ok := item["description"]; ok && argAnyString(v) != existing.Description {
			body["description"] = v
		}
		if v, ok := item["priority"]; ok && argInt(item, "priority", 0) != existing.Priority {
			body["priority"] = v
		}
		if len(body) == 0 {
			untouched = append(untouched, name)
			continue
		}
		var upd gitlabLabelObj
		ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+glabEncodeID(name), body, false, &upd)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return Fail("gitlab_label: unable to update " + name + ": " + glabErrMsg(ures)), nil
		}
		updated = append(updated, name)
		delete(byName, name)
		byName[upd.Name] = upd
	}

	if purge {
		for name, l := range byName {
			if keep[name] {
				continue
			}
			dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+glabEncodeID(name), nil, false, nil)
			if err != nil {
				return Result{}, err
			}
			if dres.RC != 0 {
				return Fail("gitlab_label: unable to purge " + name + ": " + glabErrMsg(dres)), nil
			}
			removed = append(removed, l.Name)
		}
	}

	sort.Strings(added)
	sort.Strings(updated)
	sort.Strings(removed)
	sort.Strings(untouched)
	summary := map[string]any{"added": orEmpty(added), "updated": orEmpty(updated), "removed": orEmpty(removed), "untouched": orEmpty(untouched)}

	var final []gitlabLabelObj
	fres, err := glabAPIJSON(ctx, conn, "GET", base+"?per_page=100", nil, true, &final)
	if err != nil {
		return Result{}, err
	}
	if fres.RC != 0 {
		return Fail("gitlab_label: unable to re-read labels: " + glabErrMsg(fres)), nil
	}

	changed := len(added) > 0 || len(updated) > 0 || len(removed) > 0
	r := Result{Changed: changed}
	r = r.WithExtra("labels", summary)
	return r.WithExtra("labels_obj", final), nil
}
