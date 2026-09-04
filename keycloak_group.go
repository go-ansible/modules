package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakGroup implements Ansible's `keycloak_group`
// (community.general) module: creates, updates, or deletes a Keycloak
// group (optionally nested under a chain of parent groups), via
// `kcadm.sh create/get/update/delete groups(/<id>)` and, for a
// subgroup, the dedicated `groups/<parent-id>/children` sub-resource
// (Keycloak's own `POST /admin/realms/{realm}/groups/{id}/children`
// endpoint for creating AND listing a group's immediate children,
// verified against Keycloak's own Admin REST API reference) — see
// keycloak_common.go's own doc comment for the kcadm.sh substitution.
//
// Args: id — when given, this port looks the group up directly at
// `groups/<id>` and never resolves parents/name at all (matching real
// keycloak_group's own doc: "reduces the number of API calls
// required"); name (required unless id is given for state=absent);
// parents (list of {id, name} — id preferred over name per parent,
// matching real keycloak_group's own doc, resolved top-down via
// keycloakResolveGroupPath below); attributes (dict of
// string -> string-or-list, shared with keycloak_role.go via
// normalizeAttributes/attributesEqual); state (present|absent, default
// present).
//
// Deviation from real keycloak_group: this port resolves each parents[]
// entry (and the final group itself) via an exact-name match against
// that level's own children list — real keycloak_group builds the same
// resolution through python-gitlab-equivalent recursive REST calls;
// this port's own keycloakResolveGroupPath does the identical top-down
// walk but against `groups`/`groups/<id>/children` with `-q
// search=<name>` (Keycloak's own substring search, filtered to an
// EXACT name match client-side here, since kcadm has no exact-match
// query operator) rather than an unfiltered full listing — functionally
// equivalent for the common case of non-overlapping sibling names, but
// documented here since a substring collision among sibling names could
// theoretically return more candidates than intended (still filtered
// to an exact match before use, so this cannot silently pick the wrong
// group).
func moduleKeycloakGroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_group"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_group: state must be one of present, absent, got %q", state)
	}
	name := argString(args, "name", "")
	id := argString(args, "id", "")
	if id == "" && name == "" {
		return Result{}, errArg("keycloak_group: one of id, name is required")
	}

	var current map[string]any
	var present bool
	var parentID string
	var err error
	if id != "" {
		current, present, err = kcadmShow(ctx, conn, "groups/"+id, realm)
		if err != nil {
			return Result{}, err
		}
	} else {
		id, parentID, present, err = keycloakResolveGroupPath(ctx, conn, realm, args, name)
		if err != nil {
			return Result{}, err
		}
		if present {
			current, present, err = kcadmShow(ctx, conn, "groups/"+id, realm)
			if err != nil {
				return Result{}, err
			}
		}
	}

	if state == "absent" {
		if !present {
			return Ok("Group does not exist, doing nothing"), nil
		}
		res, err := kcadmDelete(ctx, conn, "groups/"+id, realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_group", "unable to delete group", res), nil
		}
		return Changed("Group has been deleted"), nil
	}

	if name == "" {
		return Result{}, errArg("keycloak_group: name is required when creating or updating a group")
	}
	var attrs map[string][]string
	if raw, ok := args["attributes"].(map[string]any); ok {
		attrs = normalizeAttributes(raw)
	}

	if !present {
		body := map[string]any{"name": name}
		if attrs != nil {
			body["attributes"] = attrs
		}
		createPath := "groups"
		if parentID != "" {
			createPath = "groups/" + parentID + "/children"
		}
		newID, res, err := kcadmCreateBodyID(ctx, conn, createPath, realm, body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_group", "unable to create group "+name, res), nil
		}
		r := Changed("Group " + name + " has been created")
		return r.WithExtra("end_state", map[string]any{"id": newID, "name": name}), nil
	}

	var sets []string
	if have, _ := current["name"].(string); have != name {
		sets = append(sets, kcadmSet("name", name)...)
	}
	if attrs != nil && !attributesEqual(attrs, current) {
		for k, v := range attrs {
			tok, err := kcadmSetJSON("attributes."+k, v)
			if err != nil {
				return Result{}, err
			}
			sets = append(sets, tok...)
		}
	}
	if len(sets) == 0 {
		r := Ok("Group " + name + " already up to date")
		return r.WithExtra("end_state", current), nil
	}
	res, err := kcadmUpdate(ctx, conn, "groups/"+id, realm, sets, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_group", "unable to update group "+name, res), nil
	}
	return Changed("Group " + name + " has been updated"), nil
}

// keycloakResolveGroupPath walks args["parents"] top-down (each entry
// resolved by id if given, else by an exact-name match against that
// level's own children — see moduleKeycloakGroup's own doc comment),
// then looks for leafName among the resolved parent's own children (or
// the realm's own top-level groups, if there are no parents). Returns
// the leaf group's id (if found), the RESOLVED parent's id (""
// meaning top-level — used by the caller to create the group there if
// not found), and present.
func keycloakResolveGroupPath(ctx context.Context, conn remoteexec.Connection, realm string, args map[string]any, leafName string) (id, parentID string, present bool, err error) {
	parentsRaw, _ := args["parents"].([]any)
	cur := "" // "" means top-level
	for _, p := range parentsRaw {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if pid := argString(pm, "id", ""); pid != "" {
			cur = pid
			continue
		}
		pname := argString(pm, "name", "")
		childID, found, err := keycloakFindChildGroupByName(ctx, conn, realm, cur, pname)
		if err != nil {
			return "", "", false, err
		}
		if !found {
			return "", "", false, errArg("keycloak_group: parent group %q not found", pname)
		}
		cur = childID
	}
	leafID, found, err := keycloakFindChildGroupByName(ctx, conn, realm, cur, leafName)
	if err != nil {
		return "", "", false, err
	}
	return leafID, cur, found, nil
}

// keycloakFindChildGroupByName looks for a group named name directly
// under parentID's own children (parentID=="" meaning the realm's own
// top-level groups), via `-q search=<name>` filtered client-side to an
// exact match.
func keycloakFindChildGroupByName(ctx context.Context, conn remoteexec.Connection, realm, parentID, name string) (id string, found bool, err error) {
	path := "groups"
	if parentID != "" {
		path = "groups/" + parentID + "/children"
	}
	var groups []map[string]any
	res, err := kcadmGetJSON(ctx, conn, path, realm, []string{"search=" + name}, &groups)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	for _, g := range groups {
		if s, _ := g["name"].(string); s == name {
			gid, _ := g["id"].(string)
			return gid, true, nil
		}
	}
	return "", false, nil
}
