package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabUserObj is `glab api users/:id`'s own JSON object (and one
// entry of `glab api users?username=`'s own array) — only the fields
// this module reads or writes.
type gitlabUserObj struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	State    string `json:"state"`
	IsAdmin  bool   `json:"is_admin"`
	External bool   `json:"external"`
}

// gitlabSSHKeyObj is one entry of `glab api users/:id/keys`'s own JSON
// array.
type gitlabSSHKeyObj struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Key   string `json:"key"`
}

// moduleGitlabUser implements Ansible's `gitlab_user`
// (community.general) module: creates, updates, deletes, or blocks/
// unblocks a GitLab user account (an admin-only operation on real
// GitLab, matching real gitlab_user's own documented "administrator
// rights on the GitLab server" REQUIREMENT), via `glab api` against
// GitLab's own GET/POST/PUT/DELETE /users(/:id), /users/:id/block,
// /users/:id/unblock, /users/:id/keys, and /groups/:id/members — see
// gitlab_common.go's own doc comment for the `glab` substitution and
// its accepted-but-inert api_*/validate_certs/ca_path arguments. `glab`
// has no dedicated user-administration subcommand.
//
// Args: username (required); name, email (required for state=present,
// matching real gitlab_user's own doc — but, per its own NOTES, NOT
// required for state=absent/blocked/unblocked); password; isadmin
// (bool); external (bool); confirm (bool, default true) — sent as the
// API's own inverted `skip_confirmation` field; reset_password (bool);
// group (ID or path) with access_level (default guest) — adds the user
// as a group member; sshkey_name/sshkey_file/sshkey_expires_at with
// sshkey_update_mode (create|update|deduplicate, default create,
// documented per-mode below); state (present|absent|blocked|unblocked,
// default present).
//
// state=present: no matching username -> POST /users (create), then
// (if sshkey_file set) add the SSH key and (if group set) add group
// membership. A match -> PUT /users/:id with only the
// name/email/external/isadmin fields that differ, then the same SSH-key
// and group-membership reconciliation. state=absent: DELETE if a match
// exists. state=blocked/unblocked: POST /users/:id/block or /unblock,
// skipped (Changed=false) if the user's own current `state` field
// already matches.
//
// SSH key reconciliation (by sshkey_name, GitLab's own per-key `title`)
// matches real gitlab_user's own documented sshkey_update_mode
// choices exactly: create (the historical default) leaves any existing
// key(s) named sshkey_name untouched, only creating one if none exist
// yet; update replaces the single existing key of that name only if
// exactly one exists AND its key material (the algorithm+blob fields,
// ignoring any trailing comment) differs from sshkey_file; deduplicate
// deletes every existing key of that name first, then always creates a
// fresh one.
//
// Deviation from real gitlab_user: `identities` (a LIST of {provider,
// extern_uid} pairs, independently addable/removable via
// overwrite_identities) is not reconciled by this port at all — GitLab's
// user API only exposes a single `provider`/`extern_uid` pair on the
// user object itself (not a list), and this port could not verify
// python-gitlab's own multi-identity `manage_identities` call sequence
// against a live server in this sandbox; a task setting `identities` is
// accepted (for argument-shape compatibility) but has no effect. This
// is an honestly-documented gap, not a guess.
func moduleGitlabUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_user"); !ok {
		return res, nil
	}
	username, err := requireString(args, "username")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "blocked", "unblocked":
	default:
		return Result{}, errArg("gitlab_user: state must be one of present, absent, blocked, unblocked, got %q", state)
	}

	existing, found, err := gitlabFindUser(ctx, conn, username)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "absent":
		if !found {
			return Ok(username + " already absent"), nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", "users/"+strconv.Itoa(existing.ID), nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_user: unable to delete " + username + ": " + glabErrMsg(dres)), nil
		}
		return Changed(username + " deleted"), nil

	case "blocked", "unblocked":
		if !found {
			return Fail("gitlab_user: no such user: " + username), nil
		}
		wantBlocked := state == "blocked"
		isBlocked := existing.State == "blocked"
		if wantBlocked == isBlocked {
			return Ok(username + " already " + state), nil
		}
		action := "unblock"
		if wantBlocked {
			action = "block"
		}
		bres, err := glabAPIJSON(ctx, conn, "POST", "users/"+strconv.Itoa(existing.ID)+"/"+action, nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if bres.RC != 0 {
			return Fail("gitlab_user: unable to " + action + " " + username + ": " + glabErrMsg(bres)), nil
		}
		return Changed(username + " " + state), nil
	}

	// state=present
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, errArg("gitlab_user: %v", err)
	}
	email, err := requireString(args, "email")
	if err != nil {
		return Result{}, errArg("gitlab_user: %v", err)
	}
	isAdmin := argBool(args, "isadmin", false)
	external := argBool(args, "external", false)
	changed := false

	if !found {
		body := map[string]any{
			"username":          username,
			"name":              name,
			"email":             email,
			"admin":             isAdmin,
			"external":          external,
			"skip_confirmation": !argBool(args, "confirm", true),
			"reset_password":    argBool(args, "reset_password", false),
		}
		if pw := argString(args, "password", ""); pw != "" {
			body["password"] = pw
		}
		var created gitlabUserObj
		cres, err := glabAPIJSON(ctx, conn, "POST", "users", body, false, &created)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("gitlab_user: unable to create " + username + ": " + glabErrMsg(cres)), nil
		}
		existing = created
		changed = true
	} else {
		body := map[string]any{}
		if existing.Name != name {
			body["name"] = name
		}
		if existing.Email != email {
			body["email"] = email
		}
		if existing.IsAdmin != isAdmin {
			body["admin"] = isAdmin
		}
		if existing.External != external {
			body["external"] = external
		}
		if len(body) > 0 {
			var updated gitlabUserObj
			ures, err := glabAPIJSON(ctx, conn, "PUT", "users/"+strconv.Itoa(existing.ID), body, false, &updated)
			if err != nil {
				return Result{}, err
			}
			if ures.RC != 0 {
				return Fail("gitlab_user: unable to update " + username + ": " + glabErrMsg(ures)), nil
			}
			existing = updated
			changed = true
		}
	}

	if sshKeyFile := argString(args, "sshkey_file", ""); sshKeyFile != "" {
		keyChanged, err := gitlabReconcileSSHKey(ctx, conn, existing.ID, args, sshKeyFile)
		if err != nil {
			return Result{}, err
		}
		changed = changed || keyChanged
	}

	if group := argString(args, "group", ""); group != "" {
		memberChanged, err := gitlabReconcileGroupMembership(ctx, conn, group, existing.ID, argString(args, "access_level", "guest"))
		if err != nil {
			return Result{}, err
		}
		changed = changed || memberChanged
	}

	r := Result{Changed: changed}
	return r.WithExtra("user", existing), nil
}

// gitlabFindUser looks up a user by exact username via GET
// /users?username=.
func gitlabFindUser(ctx context.Context, conn remoteexec.Connection, username string) (gitlabUserObj, bool, error) {
	var users []gitlabUserObj
	res, err := glabAPIJSON(ctx, conn, "GET", "users?username="+username, nil, false, &users)
	if err != nil {
		return gitlabUserObj{}, false, err
	}
	if res.RC != 0 {
		return gitlabUserObj{}, false, nil
	}
	for _, u := range users {
		if u.Username == username {
			return u, true, nil
		}
	}
	return gitlabUserObj{}, false, nil
}

// gitlabReconcileSSHKey implements moduleGitlabUser's own documented
// sshkey_update_mode semantics — see its doc comment above for the
// three modes' exact behavior.
func gitlabReconcileSSHKey(ctx context.Context, conn remoteexec.Connection, userID int, args map[string]any, keyFile string) (bool, error) {
	name := argString(args, "sshkey_name", "")
	mode := argString(args, "sshkey_update_mode", "create")
	base := "users/" + strconv.Itoa(userID) + "/keys"

	var keys []gitlabSSHKeyObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base, nil, false, &keys)
	if err != nil {
		return false, err
	}
	if lres.RC != 0 {
		return false, fmt.Errorf("gitlab_user: unable to list ssh keys: %s", glabErrMsg(lres))
	}
	var matches []gitlabSSHKeyObj
	for _, k := range keys {
		if k.Title == name {
			matches = append(matches, k)
		}
	}

	create := func() (bool, error) {
		body := map[string]any{"title": name, "key": keyFile}
		if exp := argString(args, "sshkey_expires_at", ""); exp != "" {
			body["expires_at"] = exp
		}
		cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, nil)
		if err != nil {
			return false, err
		}
		if cres.RC != 0 {
			return false, fmt.Errorf("gitlab_user: unable to add ssh key %s: %s", name, glabErrMsg(cres))
		}
		return true, nil
	}

	switch mode {
	case "deduplicate":
		changed := false
		for _, k := range matches {
			dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(k.ID), nil, false, nil)
			if err != nil {
				return false, err
			}
			if dres.RC != 0 {
				return false, fmt.Errorf("gitlab_user: unable to remove ssh key %s: %s", name, glabErrMsg(dres))
			}
			changed = true
		}
		created, err := create()
		if err != nil {
			return false, err
		}
		return changed || created, nil

	case "update":
		if len(matches) == 0 {
			return create()
		}
		if len(matches) != 1 {
			return false, nil
		}
		if gitlabSSHKeyMaterialEqual(matches[0].Key, keyFile) {
			return false, nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(matches[0].ID), nil, false, nil)
		if err != nil {
			return false, err
		}
		if dres.RC != 0 {
			return false, fmt.Errorf("gitlab_user: unable to replace ssh key %s: %s", name, glabErrMsg(dres))
		}
		return create()

	default: // "create"
		if len(matches) > 0 {
			return false, nil
		}
		return create()
	}
}

// gitlabSSHKeyMaterialEqual compares two SSH public keys' own
// algorithm+base64-blob fields, ignoring a trailing comment — matching
// real gitlab_user's own documented "SSH key comments are ignored when
// comparing key material".
func gitlabSSHKeyMaterialEqual(a, b string) bool {
	fa := strings.Fields(a)
	fb := strings.Fields(b)
	if len(fa) < 2 || len(fb) < 2 {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return fa[0] == fb[0] && fa[1] == fb[1]
}

// gitlabReconcileGroupMembership adds userID to group (resolved via
// glabResolveGroupID) at accessLevel, or updates its access level if
// already a member with a different one; a no-op if already a member at
// the requested level.
func gitlabReconcileGroupMembership(ctx context.Context, conn remoteexec.Connection, group string, userID int, accessLevelName string) (bool, error) {
	gid, err := glabResolveGroupID(ctx, conn, group)
	if err != nil {
		return false, err
	}
	level, err := glabAccessLevel(accessLevelName)
	if err != nil {
		return false, errArg("gitlab_user: access_level: %v", err)
	}
	base := "groups/" + strconv.Itoa(gid) + "/members"

	var member struct {
		AccessLevel int `json:"access_level"`
	}
	gres, err := glabAPIJSON(ctx, conn, "GET", base+"/"+strconv.Itoa(userID), nil, false, &member)
	if err != nil {
		return false, err
	}
	if gres.RC == 0 {
		if member.AccessLevel == level {
			return false, nil
		}
		ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+strconv.Itoa(userID), map[string]any{"access_level": level}, false, nil)
		if err != nil {
			return false, err
		}
		if ures.RC != 0 {
			return false, fmt.Errorf("gitlab_user: unable to update group membership: %s", glabErrMsg(ures))
		}
		return true, nil
	}
	if !glabIsNotFound(gres) {
		return false, fmt.Errorf("gitlab_user: unable to read group membership: %s", glabErrMsg(gres))
	}
	cres, err := glabAPIJSON(ctx, conn, "POST", base, map[string]any{"user_id": userID, "access_level": level}, false, nil)
	if err != nil {
		return false, err
	}
	if cres.RC != 0 {
		return false, fmt.Errorf("gitlab_user: unable to add group membership: %s", glabErrMsg(cres))
	}
	return true, nil
}
