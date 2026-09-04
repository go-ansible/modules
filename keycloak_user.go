package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakUser implements Ansible's `keycloak_user`
// (community.general) module: creates, updates, or deletes a Keycloak
// user account, via `kcadm.sh create/get/update/delete users(/<id>)` —
// see keycloak_common.go's own doc comment for the kcadm.sh
// substitution.
//
// Args: username (required unless id/state=absent); id — when given,
// used directly, skipping a username lookup (matching real
// keycloak_user's own doc: "the module does not modify the user ID of
// an existing user"); realm (default master); first_name/last_name/
// email/enabled; email_verified + email_verified_behavior
// (compatibility|no_defaults, default compatibility) — matching real
// keycloak_user's own documented legacy quirk: under `compatibility`
// (the default), an OMITTED email_verified is still sent as `false`
// (real keycloak_user's own historical default), touching that field
// even when the task never mentions it; `no_defaults` instead leaves it
// untouched when omitted; attributes (list of {name, values, state} —
// state=absent REMOVES that attribute entirely, matching real
// keycloak_user's own per-attribute state convention, distinct from
// keycloak_group/keycloak_role's own whole-dict attributes shape);
// credentials (list of {type, value, temporary} — only type=="password"
// entries are applied, each one via `kcadm.sh update
// users/<id>/reset-password -r <realm> -f -`, piping the credential
// representation `{"type":"password","value":..,"temporary":..}` as
// the raw JSON body over STDIN, matching real keycloak_user's own
// underlying `PUT .../reset-password` call — chosen deliberately over
// kcadm's own dedicated `set-password` convenience command, whose only
// non-interactive way to supply a password is a `--new-password` ARGV
// flag, which would place a secret on the command line; `-f -` avoids
// that entirely, matching this project's own hard "no secrets in argv"
// rule); groups (list of {name, state} — name may be a bare group name
// or a "/parent/child" path, resolved the same top-down way
// keycloak_group.go's own keycloakResolveGroupPath does, via
// `kcadm.sh update users/<id>/groups/<gid>`/`kcadm.sh delete
// users/<id>/groups/<gid>` — Keycloak's own dedicated
// join/leave-group endpoints); required_actions (full-replace list);
// force (bool — when true and the user already exists, this port
// deletes and recreates it unconditionally, matching real
// keycloak_user's own documented "allows to remove user and recreate
// it"); state (present|absent, default present).
//
// Deviation from real keycloak_user: access (a read-only dict reported
// BY Keycloak, not a real input), client_consents, disableable_
// credential_types, federated_identities, origin, self, and
// service_account_client_id are accepted (for argument-shape
// compatibility) but have NO EFFECT on this port's behavior — an
// honestly-documented gap for arguments this port judged too rarely
// used to justify the added complexity within this batch's time
// budget, rather than a silent misinterpretation.
func moduleKeycloakUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_user"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_user: state must be one of present, absent, got %q", state)
	}
	username := argString(args, "username", "")
	id := argString(args, "id", "")
	if id == "" && username == "" {
		return Result{}, errArg("keycloak_user: username is required")
	}

	var current map[string]any
	var present bool
	var err error
	if id != "" {
		current, present, err = kcadmShow(ctx, conn, "users/"+id, realm)
	} else {
		id, present, err = kcadmFindUserByUsername(ctx, conn, realm, username)
		if err == nil && present {
			current, present, err = kcadmShow(ctx, conn, "users/"+id, realm)
		}
	}
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok("User does not exist, doing nothing"), nil
		}
		res, err := kcadmDelete(ctx, conn, "users/"+id, realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_user", "unable to delete user", res), nil
		}
		return Changed("User has been removed"), nil
	}

	if username == "" {
		username, _ = current["username"].(string)
	}

	force := argBool(args, "force", false)
	if present && force {
		res, err := kcadmDelete(ctx, conn, "users/"+id, realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_user", "unable to remove user "+username+" for recreation", res), nil
		}
		present = false
		current = nil
	}

	body := map[string]any{"username": username}
	if v, ok := args["first_name"]; ok {
		body["firstName"] = argString(map[string]any{"v": v}, "v", "")
	}
	if v, ok := args["last_name"]; ok {
		body["lastName"] = argString(map[string]any{"v": v}, "v", "")
	}
	if v, ok := args["email"]; ok {
		body["email"] = argString(map[string]any{"v": v}, "v", "")
	}
	if _, ok := args["enabled"]; ok {
		body["enabled"] = argBool(args, "enabled", false)
	}

	behavior := argString(args, "email_verified_behavior", "compatibility")
	if _, ok := args["email_verified"]; ok {
		body["emailVerified"] = argBool(args, "email_verified", false)
	} else if behavior != "no_defaults" {
		body["emailVerified"] = false
	}

	if _, ok := args["required_actions"]; ok {
		body["requiredActions"] = argStringList(args, "required_actions")
	}

	attrs, attrsGiven := keycloakUserDesiredAttributes(args, current)
	if attrsGiven {
		body["attributes"] = attrs
	}

	userCreated := false
	if !present {
		newID, res, err := kcadmCreateBodyID(ctx, conn, "users", realm, body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_user", "unable to create user "+username, res), nil
		}
		id = newID
		userCreated = true
	} else {
		merged := map[string]any{}
		for k, v := range current {
			merged[k] = v
		}
		changed := false
		for k, v := range body {
			if have, existed := merged[k]; !existed || !jsonScalarEqual(v, have) {
				merged[k] = v
				changed = true
			}
		}
		if changed {
			res, err := kcadmUpdateBody(ctx, conn, "users/"+id, realm, merged)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return kcadmFailedf("keycloak_user", "unable to update user "+username, res), nil
			}
		}
	}

	if err := keycloakApplyUserCredentials(ctx, conn, realm, id, args); err != nil {
		return Result{}, err
	}
	if err := keycloakApplyUserGroups(ctx, conn, realm, id, args); err != nil {
		return Result{}, err
	}

	r := Changed("User " + username + " has been " + map[bool]string{true: "created", false: "updated"}[userCreated])
	r = r.WithExtra("user_created", userCreated)
	return r, nil
}

// keycloakUserDesiredAttributes builds the final attributes map from
// args["attributes"] (a list of {name, values, state}) layered onto
// current's own existing attributes — see moduleKeycloakUser's own doc
// comment on why keycloak_user's own attribute shape differs from
// keycloak_group/keycloak_role's.
func keycloakUserDesiredAttributes(args map[string]any, current map[string]any) (map[string][]string, bool) {
	list, ok := args["attributes"].([]any)
	if !ok {
		return nil, false
	}
	out := map[string][]string{}
	if current != nil {
		if haveRaw, ok := current["attributes"].(map[string]any); ok {
			for k, v := range haveRaw {
				out[k] = decodeStringSlice(v)
			}
		}
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := argString(m, "name", "")
		if name == "" {
			continue
		}
		if argString(m, "state", "present") == "absent" {
			delete(out, name)
			continue
		}
		out[name] = argStringList(m, "values")
	}
	return out, true
}

// keycloakApplyUserCredentials applies every type=="password" entry of
// args["credentials"], in order — see moduleKeycloakUser's own doc
// comment for why this goes through the generic reset-password REST
// endpoint via a stdin-piped body rather than kcadm's own set-password
// convenience command.
func keycloakApplyUserCredentials(ctx context.Context, conn remoteexec.Connection, realm, id string, args map[string]any) error {
	list, _ := args["credentials"].([]any)
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if argString(m, "type", "") != "password" {
			continue
		}
		body := map[string]any{
			"type":      "password",
			"value":     argString(m, "value", ""),
			"temporary": argBool(m, "temporary", false),
		}
		res, err := kcadmUpdateBody(ctx, conn, "users/"+id+"/reset-password", realm, body)
		if err != nil {
			return err
		}
		if res.RC != 0 {
			return errArg("keycloak_user: unable to set password credential: %s", res.Stderr)
		}
	}
	return nil
}

// keycloakApplyUserGroups reconciles args["groups"] (list of {name,
// state}) against id's own current group membership, via `kcadm.sh
// update users/<id>/groups/<gid>` (join) / `kcadm.sh delete
// users/<id>/groups/<gid>` (leave) — Keycloak's own dedicated
// membership endpoints.
func keycloakApplyUserGroups(ctx context.Context, conn remoteexec.Connection, realm, id string, args map[string]any) error {
	list, ok := args["groups"].([]any)
	if !ok {
		return nil
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := argString(m, "name", "")
		if name == "" {
			continue
		}
		gid, found, err := keycloakResolveGroupByPath(ctx, conn, realm, name)
		if err != nil {
			return err
		}
		if !found {
			return errArg("keycloak_user: no such group: %s", name)
		}
		want := argString(m, "state", "present")
		if want == "absent" {
			res, err := kcadmDelete(ctx, conn, "users/"+id+"/groups/"+gid, realm)
			if err != nil {
				return err
			}
			if res.RC != 0 {
				return errArg("keycloak_user: unable to remove user from group %s: %s", name, res.Stderr)
			}
			continue
		}
		res, err := kcadmUpdateBody(ctx, conn, "users/"+id+"/groups/"+gid, realm, map[string]any{})
		if err != nil {
			return err
		}
		if res.RC != 0 {
			return errArg("keycloak_user: unable to add user to group %s: %s", name, res.Stderr)
		}
	}
	return nil
}

// keycloakResolveGroupByPath resolves a bare group name or a
// "/parent/child" path to its own group id, walking top-down via
// keycloakFindChildGroupByName (shared with keycloak_group.go).
func keycloakResolveGroupByPath(ctx context.Context, conn remoteexec.Connection, realm, path string) (id string, found bool, err error) {
	segments := splitGroupPath(path)
	if len(segments) == 0 {
		return "", false, nil
	}
	cur := ""
	for _, seg := range segments {
		childID, ok, err := keycloakFindChildGroupByName(ctx, conn, realm, cur, seg)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return "", false, nil
		}
		cur = childID
	}
	return cur, true, nil
}

// splitGroupPath splits a "/parent/child" path (or a bare name, with no
// leading slash) into its non-empty segments.
func splitGroupPath(path string) []string {
	var segs []string
	cur := ""
	for _, r := range path {
		if r == '/' {
			if cur != "" {
				segs = append(segs, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		segs = append(segs, cur)
	}
	return segs
}
