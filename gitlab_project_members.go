package modules

import (
	"context"
	"sort"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabMemberObj is one entry of `glab api
// projects/:id/members/all`'s own JSON array.
type gitlabMemberObj struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	AccessLevel int    `json:"access_level"`
}

// gitlabDesiredMember is one resolved (username, access-level-name)
// pair moduleGitlabProjectMembers reconciles against a project's
// current membership, built from either gitlab_user+access_level or
// gitlab_users_access.
type gitlabDesiredMember struct {
	Username    string
	AccessLevel string
}

// moduleGitlabProjectMembers implements Ansible's
// `gitlab_project_members` (community.general) module: adds, removes,
// or changes the access level of project members, via `glab api`
// against GitLab's own GET /projects/:id/members/all and POST/PUT/
// DELETE /projects/:id/members(/:user_id) — see gitlab_common.go's own
// doc comment for the `glab` substitution and its accepted-but-inert
// api_*/validate_certs/ca_path arguments. `glab` has no dedicated
// project-membership subcommand.
//
// Args: project (required); gitlab_user (list of usernames, mutually
// exclusive with gitlab_users_access) with access_level (required with
// gitlab_user, applied to every listed user) — OR gitlab_users_access
// (list of {name, access_level} dicts, mutually exclusive with
// gitlab_user/access_level); purge_users (list of access-level names) —
// removes any CURRENT member at one of those access levels who is not
// in the desired set (state=present only, matching real
// gitlab_project_members's own documented purge scope: it purges by
// access level, not unconditionally); state (present|absent, default
// present).
//
// Each username is resolved to a numeric user ID via glabResolveUserID
// (GET /users?username=) — a task naming a username with no matching
// GitLab user fails the whole module (Result{Failed:true}), matching
// real gitlab_project_members's own behavior (it cannot add a member
// who does not exist).
//
// state=present: a desired user not currently a member is added (POST);
// a desired user already a member with a DIFFERENT access level is
// updated (PUT); already matching is a no-op. state=absent: each named
// user currently a member is removed (DELETE); not a member is a no-op.
func moduleGitlabProjectMembers(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_project_members"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_project_members: state must be one of present, absent, got %q", state)
	}
	base := "projects/" + glabEncodeID(project) + "/members"

	desired, err := gitlabMembersDesiredList(args)
	if err != nil {
		return Result{}, err
	}
	if len(desired) == 0 {
		return Result{}, errArg("gitlab_project_members: one of gitlab_user, gitlab_users_access is required")
	}

	var current []gitlabMemberObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base+"/all?per_page=100", nil, true, &current)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_project_members: unable to list members: " + glabErrMsg(lres)), nil
	}
	byUsername := map[string]gitlabMemberObj{}
	for _, m := range current {
		byUsername[m.Username] = m
	}

	changed := false
	desiredNames := map[string]bool{}
	for _, d := range desired {
		desiredNames[d.Username] = true
		uid, found, err := glabResolveUserID(ctx, conn, d.Username)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail("gitlab_project_members: no such GitLab user: " + d.Username), nil
		}

		if state == "absent" {
			if _, isMember := byUsername[d.Username]; isMember {
				dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(uid), nil, false, nil)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("gitlab_project_members: unable to remove " + d.Username + ": " + glabErrMsg(dres)), nil
				}
				changed = true
				delete(byUsername, d.Username)
			}
			continue
		}

		level, err := glabAccessLevel(d.AccessLevel)
		if err != nil {
			return Result{}, errArg("gitlab_project_members: %v", err)
		}
		if m, isMember := byUsername[d.Username]; isMember {
			if m.AccessLevel != level {
				body := map[string]any{"access_level": level}
				ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+strconv.Itoa(uid), body, false, nil)
				if err != nil {
					return Result{}, err
				}
				if ures.RC != 0 {
					return Fail("gitlab_project_members: unable to update " + d.Username + ": " + glabErrMsg(ures)), nil
				}
				changed = true
			}
			continue
		}
		body := map[string]any{"user_id": uid, "access_level": level}
		cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, nil)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("gitlab_project_members: unable to add " + d.Username + ": " + glabErrMsg(cres)), nil
		}
		changed = true
	}

	if state == "present" {
		purgeLevels := map[string]bool{}
		for _, lvl := range argStringList(args, "purge_users") {
			purgeLevels[lvl] = true
		}
		if len(purgeLevels) > 0 {
			levelNames := gitlabSortedKeys(purgeLevels)
			for _, m := range current {
				if desiredNames[m.Username] {
					continue
				}
				matches := false
				for _, name := range levelNames {
					lvl, err := glabAccessLevel(name)
					if err == nil && lvl == m.AccessLevel {
						matches = true
						break
					}
				}
				if !matches {
					continue
				}
				dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(m.ID), nil, false, nil)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("gitlab_project_members: unable to purge " + m.Username + ": " + glabErrMsg(dres)), nil
				}
				changed = true
			}
		}
	}

	if changed {
		return Changed("membership updated"), nil
	}
	return Ok("membership already up to date"), nil
}

// gitlabMembersDesiredList normalizes gitlab_user+access_level (a
// username or list of usernames, all sharing one access_level) and
// gitlab_users_access (a per-user access_level list) — mutually
// exclusive per real gitlab_project_members's own doc — into one
// []gitlabDesiredMember.
func gitlabMembersDesiredList(args map[string]any) ([]gitlabDesiredMember, error) {
	users := argStringList(args, "gitlab_user")
	usersAccess, _ := args["gitlab_users_access"].([]any)
	if len(users) > 0 && len(usersAccess) > 0 {
		return nil, errArg("gitlab_project_members: gitlab_user and gitlab_users_access are mutually exclusive")
	}
	var out []gitlabDesiredMember
	if len(users) > 0 {
		level := argString(args, "access_level", "")
		if level == "" && argString(args, "state", "present") == "present" {
			return nil, errArg("gitlab_project_members: access_level is required when gitlab_user is used with state=present")
		}
		for _, u := range users {
			out = append(out, gitlabDesiredMember{Username: u, AccessLevel: level})
		}
		return out, nil
	}
	for _, ra := range usersAccess {
		item, ok := ra.(map[string]any)
		if !ok {
			return nil, errArg("gitlab_project_members: each entry of gitlab_users_access must be a dict")
		}
		name, err := requireString(item, "name")
		if err != nil {
			return nil, errArg("gitlab_project_members: %v", err)
		}
		level := argString(item, "access_level", "")
		out = append(out, gitlabDesiredMember{Username: name, AccessLevel: level})
	}
	return out, nil
}

// gitlabSortedKeys returns the sorted keys of a string set, used for
// deterministic purge-level iteration.
func gitlabSortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
