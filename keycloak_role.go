package modules

import (
	"context"
	"encoding/json"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakRole implements Ansible's `keycloak_role`
// (community.general) module: creates, updates, or deletes a realm or
// client role, via `kcadm.sh create/get/update/delete roles/<name>`
// (realm role) or `clients/<client-uuid>/roles/<name>` (client role,
// after resolving client_id -> the client's own internal UUID through
// `kcadm.sh get clients -r <realm> -q clientId=<client_id>`) — see
// keycloak_common.go's own doc comment for the kcadm.sh substitution.
//
// Args: name (required); realm (default master); client_id — if set,
// the role belongs to that client (resolved to its internal ID first);
// if absent, the role is a realm role; description; composite (bool,
// default false); composites (list of {name, client_id, state} —
// state=absent REMOVES that role from the composite set instead of
// adding it, matching real keycloak_role's own doc); attributes (dict
// of string -> string-or-list, matching real keycloak_role's own
// multi-valued-attribute convention, shared with keycloak_group.go via
// normalizeAttributes/attributesEqual); state (present|absent, default
// present).
//
// Composites are reconciled against the role's own current composite
// set (`kcadm.sh get roles/<name>/composites`), resolving each desired
// {name, client_id} pair to its own role representation ID first (a
// realm role via `roles/<name>`, a client role via
// `clients/<uuid>/roles/<name>` after the same client_id resolution),
// then POSTing/DELETEing `roles/<name>/composites` with the resolved
// `[{"id": "..."}]` body — a role named in `composites` that does not
// itself exist is a Result{Failed:true}, matching real keycloak_role's
// own behavior (it cannot compose a role that is not there).
func moduleKeycloakRole(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_role"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_role: state must be one of present, absent, got %q", state)
	}
	clientID := argString(args, "client_id", "")

	basePath := "roles"
	if clientID != "" {
		cuuid, found, err := kcadmFindClientByClientID(ctx, conn, realm, clientID)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail("keycloak_role: no such client: " + clientID), nil
		}
		basePath = "clients/" + cuuid + "/roles"
	}
	rolePath := basePath + "/" + name

	current, present, err := kcadmShow(ctx, conn, rolePath, realm)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok("Role " + name + " does not exist"), nil
		}
		res, err := kcadmDelete(ctx, conn, rolePath, realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_role", "unable to delete role "+name, res), nil
		}
		return Changed("Role " + name + " has been deleted"), nil
	}

	var attrs map[string][]string
	if raw, ok := args["attributes"].(map[string]any); ok {
		attrs = normalizeAttributes(raw)
	}

	if !present {
		sets := kcadmSet("name", name)
		if d := argString(args, "description", ""); d != "" {
			sets = append(sets, kcadmSet("description", d)...)
		}
		if _, ok := args["composite"]; ok {
			sets = append(sets, kcadmSetBool("composite", argBool(args, "composite", false))...)
		}
		for k, v := range attrs {
			tok, err := kcadmSetJSON("attributes."+k, v)
			if err != nil {
				return Result{}, err
			}
			sets = append(sets, tok...)
		}
		res, err := kcadmCreate(ctx, conn, basePath, realm, sets)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_role", "unable to create role "+name, res), nil
		}
		if err := keycloakReconcileComposites(ctx, conn, realm, rolePath, args); err != nil {
			return Result{}, err
		}
		return Changed("Role " + name + " has been created"), nil
	}

	var sets, deletes []string
	if _, ok := args["description"]; ok {
		if fs := argString(args, "description", ""); fs != current["description"] {
			sets = append(sets, kcadmSet("description", fs)...)
		}
	}
	if _, ok := args["composite"]; ok {
		want := argBool(args, "composite", false)
		if have, _ := current["composite"].(bool); have != want {
			sets = append(sets, kcadmSetBool("composite", want)...)
		}
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

	changed := false
	if len(sets) > 0 || len(deletes) > 0 {
		res, err := kcadmUpdate(ctx, conn, rolePath, realm, sets, deletes)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_role", "unable to update role "+name, res), nil
		}
		changed = true
	}
	compChanged, err := keycloakReconcileCompositesChanged(ctx, conn, realm, rolePath, args)
	if err != nil {
		return Result{}, err
	}
	changed = changed || compChanged

	if changed {
		return Changed("Role " + name + " has been updated"), nil
	}
	return Ok("Role " + name + " already up to date"), nil
}

// kcadmFindClientByClientID resolves a client's clientId (its
// human-readable name, e.g. "my-app") to its own internal UUID via
// `kcadm.sh get clients -r <realm> -q clientId=<clientID>`.
func kcadmFindClientByClientID(ctx context.Context, conn remoteexec.Connection, realm, clientID string) (uuid string, found bool, err error) {
	var clients []map[string]any
	res, err := kcadmGetJSON(ctx, conn, "clients", realm, []string{"clientId=" + clientID}, &clients)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	for _, c := range clients {
		if s, _ := c["clientId"].(string); s == clientID {
			id, _ := c["id"].(string)
			return id, true, nil
		}
	}
	return "", false, nil
}

// keycloakRoleRef resolves one {name, client_id} composite reference to
// its own role representation (needed to get its "id" for a
// composites add/remove body).
func keycloakRoleRef(ctx context.Context, conn remoteexec.Connection, realm, name, clientID string) (map[string]any, bool, error) {
	path := "roles/" + name
	if clientID != "" {
		cuuid, found, err := kcadmFindClientByClientID(ctx, conn, realm, clientID)
		if err != nil || !found {
			return nil, false, err
		}
		path = "clients/" + cuuid + "/roles/" + name
	}
	return kcadmShow(ctx, conn, path, realm)
}

// keycloakReconcileComposites adds every entry of args["composites"]
// with state!=absent to rolePath's own composite set — used on create,
// where there is no existing composite set to diff against yet.
func keycloakReconcileComposites(ctx context.Context, conn remoteexec.Connection, realm, rolePath string, args map[string]any) error {
	list, _ := args["composites"].([]any)
	var toAdd []map[string]any
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if argString(m, "state", "present") == "absent" {
			continue
		}
		cname := argString(m, "name", "")
		cclient := argStringAliased(m, "client_id", "clientId", "")
		ref, found, err := keycloakRoleRef(ctx, conn, realm, cname, cclient)
		if err != nil {
			return err
		}
		if !found {
			return errArg("keycloak_role: composite role %q not found", cname)
		}
		toAdd = append(toAdd, map[string]any{"id": ref["id"]})
	}
	if len(toAdd) == 0 {
		return nil
	}
	res, err := kcadmCreateBody(ctx, conn, rolePath+"/composites", realm, toAdd)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return errArg("keycloak_role: unable to add composites: %s", res.Stderr)
	}
	return nil
}

// keycloakReconcileCompositesChanged diffs args["composites"] against
// rolePath's own current composite set and adds/removes only what
// differs — used on update.
func keycloakReconcileCompositesChanged(ctx context.Context, conn remoteexec.Connection, realm, rolePath string, args map[string]any) (bool, error) {
	list, ok := args["composites"].([]any)
	if !ok {
		return false, nil
	}
	var current []map[string]any
	res, err := kcadmGetJSON(ctx, conn, rolePath+"/composites", realm, nil, &current)
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		current = nil
	}
	currentIDs := map[string]bool{}
	for _, c := range current {
		if id, _ := c["id"].(string); id != "" {
			currentIDs[id] = true
		}
	}

	var toAdd, toRemove []map[string]any
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cname := argString(m, "name", "")
		cclient := argStringAliased(m, "client_id", "clientId", "")
		ref, found, err := keycloakRoleRef(ctx, conn, realm, cname, cclient)
		if err != nil {
			return false, err
		}
		if !found {
			return false, errArg("keycloak_role: composite role %q not found", cname)
		}
		id, _ := ref["id"].(string)
		wantAbsent := argString(m, "state", "present") == "absent"
		has := currentIDs[id]
		if wantAbsent && has {
			toRemove = append(toRemove, map[string]any{"id": id})
		} else if !wantAbsent && !has {
			toAdd = append(toAdd, map[string]any{"id": id})
		}
	}

	changed := false
	if len(toAdd) > 0 {
		res, err := kcadmCreateBody(ctx, conn, rolePath+"/composites", realm, toAdd)
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, errArg("keycloak_role: unable to add composites: %s", res.Stderr)
		}
		changed = true
	}
	if len(toRemove) > 0 {
		b, jerr := json.Marshal(toRemove)
		if jerr != nil {
			return false, jerr
		}
		res, err := kcadmRunStdin(ctx, conn, string(b), "delete", rolePath+"/composites", "-r", realm, "-f", "-")
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, errArg("keycloak_role: unable to remove composites: %s", res.Stderr)
		}
		changed = true
	}
	return changed, nil
}
