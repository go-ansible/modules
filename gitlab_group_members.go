package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGitlabGroupMembers implements Ansible's `gitlab_group_members`
// (community.general) module: adds, removes, or changes the access
// level of group members, via `glab api` against GitLab's own GET
// /groups/:id/members/all and POST/PUT/DELETE
// /groups/:id/members(/:user_id) — see gitlab_common.go's own doc
// comment for the `glab` substitution and its accepted-but-inert
// api_*/validate_certs/ca_path arguments. `glab` has no dedicated
// group-membership subcommand.
//
// This module's arguments (gitlab_user, gitlab_users_access,
// access_level, purge_users, state) are identically shaped to real
// gitlab_project_members's own — this batch's sibling
// gitlab_project_members.go already normalizes exactly that shape via
// its own gitlabMembersDesiredList, so this module reuses it (and its
// gitlabMemberObj/gitlabDesiredMember/gitlabSortedKeys types/helpers)
// directly rather than duplicating it; the only difference from
// gitlab_project_members is the resource identifier argument's own
// name (gitlab_group, a group's full_path, not project) and the API
// base path (groups/:id/members, not projects/:id/members). Error
// messages surfaced from gitlabMembersDesiredList itself still read
// "gitlab_project_members: ..." in the rare malformed-input case (e.g.
// both gitlab_user and gitlab_users_access given) — a known, harmless
// cosmetic mismatch from reusing a sibling module's own validator
// rather than a functional one.
//
// Args: gitlab_group (required, full_path); gitlab_user (list of
// usernames, mutually exclusive with gitlab_users_access) with
// access_level (required with gitlab_user, applied to every listed
// user) — OR gitlab_users_access (list of {name, access_level} dicts,
// mutually exclusive with gitlab_user/access_level); purge_users (list
// of access-level names) — removes any CURRENT member at one of those
// access levels who is not in the desired set (state=present only,
// matching real gitlab_group_members's own documented purge scope: it
// purges by access level, not unconditionally); state (present|absent,
// default present).
//
// Each username is resolved to a numeric user ID via glabResolveUserID
// (GET /users?username=) — a task naming a username with no matching
// GitLab user fails the whole module (Result{Failed:true}), matching
// real gitlab_group_members's own behavior (it cannot add a member who
// does not exist).
//
// state=present: a desired user not currently a member is added (POST);
// a desired user already a member with a DIFFERENT access level is
// updated (PUT); already matching is a no-op. state=absent: each named
// user currently a member is removed (DELETE); not a member is a no-op.
func moduleGitlabGroupMembers(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_group_members"); !ok {
		return res, nil
	}
	group, err := requireString(args, "gitlab_group")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_group_members: state must be one of present, absent, got %q", state)
	}
	base := "groups/" + glabEncodeID(group) + "/members"

	desired, err := gitlabMembersDesiredList(args)
	if err != nil {
		return Result{}, err
	}
	if len(desired) == 0 {
		return Result{}, errArg("gitlab_group_members: one of gitlab_user, gitlab_users_access is required")
	}

	var current []gitlabMemberObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base+"/all?per_page=100", nil, true, &current)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_group_members: unable to list members: " + glabErrMsg(lres)), nil
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
			return Fail("gitlab_group_members: no such GitLab user: " + d.Username), nil
		}

		if state == "absent" {
			if _, isMember := byUsername[d.Username]; isMember {
				dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(uid), nil, false, nil)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("gitlab_group_members: unable to remove " + d.Username + ": " + glabErrMsg(dres)), nil
				}
				changed = true
				delete(byUsername, d.Username)
			}
			continue
		}

		level, err := glabAccessLevel(d.AccessLevel)
		if err != nil {
			return Result{}, errArg("gitlab_group_members: %v", err)
		}
		if m, isMember := byUsername[d.Username]; isMember {
			if m.AccessLevel != level {
				body := map[string]any{"access_level": level}
				ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+strconv.Itoa(uid), body, false, nil)
				if err != nil {
					return Result{}, err
				}
				if ures.RC != 0 {
					return Fail("gitlab_group_members: unable to update " + d.Username + ": " + glabErrMsg(ures)), nil
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
			return Fail("gitlab_group_members: unable to add " + d.Username + ": " + glabErrMsg(cres)), nil
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
					return Fail("gitlab_group_members: unable to purge " + m.Username + ": " + glabErrMsg(dres)), nil
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
