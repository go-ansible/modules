package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClientRolemapping implements Ansible's
// `keycloak_client_rolemapping` (community.general) module: maps or
// unmaps a CLIENT's own roles onto a GROUP, via kcadm.sh's own
// `groups/<gid>/role-mappings/clients/<cid>` resource path — GET to
// read, POST/DELETE (both with a JSON array of role representations —
// see keycloak_json_helpers.go's own kcadmDeleteBody doc comment) to
// mutate — verified against module_utils/_keycloak.py's own
// URL_CLIENT_GROUP_ROLEMAPPINGS.
//
// Args: realm (default master); cid (the client's own internal id —
// skips a lookup when given) OR client_id (the client's own clientId,
// resolved via kcResolveClientID); gid (the group's own internal id —
// skips a lookup when given) OR group_name (resolved by walking
// `parents` — each identified by id (preferred) or name, top to
// bottom, via `groups/<id>/children` — then searching that last
// parent's own children, or the realm's own top-level groups when
// `parents` is empty, for group_name); roles (required, list of
// dicts: id (skips a lookup when given) and/or name — resolved
// against the client's own roles list when id is absent); state
// (present|absent, default present).
//
// Idempotency: current group/client role mappings are fetched, and
// only roles actually missing (state=present) or actually present
// (state=absent) from `roles` are sent.
func moduleKeycloakClientRolemapping(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_client_rolemapping"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	roles := argListOfMaps(args, "roles")

	cid := argString(args, "cid", "")
	if cid == "" {
		clientID, err := requireString(args, "client_id")
		if err != nil {
			return Result{}, err
		}
		resolved, found, err := kcResolveClientID(ctx, conn, realm, clientID)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail(fmt.Sprintf("%s: client %q not found in realm %s", mod, clientID, realm)), nil
		}
		cid = resolved
	}

	gid := argString(args, "gid", "")
	if gid == "" {
		groupName, err := requireString(args, "group_name")
		if err != nil {
			return Result{}, err
		}
		resolved, err := kcResolveGroupID(ctx, conn, realm, groupName, argListOfMaps(args, "parents"))
		if err != nil {
			return Result{}, err
		}
		if resolved == "" {
			return Fail(fmt.Sprintf("%s: group %q not found in realm %s", mod, groupName, realm)), nil
		}
		gid = resolved
	}

	clientRoles, err := kcListPath(ctx, conn, realm, "clients/"+cid+"/roles")
	if err != nil {
		return Result{}, err
	}
	current, err := kcListPath(ctx, conn, realm, "groups/"+gid+"/role-mappings/clients/"+cid)
	if err != nil {
		return Result{}, err
	}
	currentByID := map[string]bool{}
	for _, r := range current {
		currentByID[kcString(r, "id")] = true
	}

	var toApply []map[string]any
	for _, want := range roles {
		roleID := kcString(want, "id")
		name := kcString(want, "name")
		if roleID == "" {
			role := kcFindByField(clientRoles, "name", name)
			if role == nil {
				return Fail(fmt.Sprintf("%s: role %q not found on client", mod, name)), nil
			}
			roleID = kcString(role, "id")
		}
		has := currentByID[roleID]
		if (state == "present" && !has) || (state == "absent" && has) {
			toApply = append(toApply, map[string]any{"id": roleID, "name": name})
		}
	}

	if len(toApply) == 0 {
		return Ok("Client role mapping already up to date").WithExtra("end_state", current), nil
	}

	path := "groups/" + gid + "/role-mappings/clients/" + cid
	if state == "present" {
		res, err := kcadmCreateBody(ctx, conn, path, realm, toApply)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "assign client roles to group", res), nil
		}
	} else {
		res, err := kcadmDeleteBody(ctx, conn, path, realm, toApply)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "unassign client roles from group", res), nil
		}
	}
	final, err := kcListPath(ctx, conn, realm, path)
	if err != nil {
		return Result{}, err
	}
	return Changed("Client role mapping has been updated").WithExtra("end_state", final), nil
}

// kcResolveGroupID resolves a group's own internal id by walking
// parents (each a dict with "id" (preferred) or "name") from the top,
// descending via `groups/<id>/children`, then searching the final
// parent's own children (or the realm's own top-level groups when
// parents is empty) for groupName. Returns "" (no error) if not found.
func kcResolveGroupID(ctx context.Context, conn remoteexec.Connection, realm, groupName string, parents []map[string]any) (string, error) {
	parentID := ""
	for _, p := range parents {
		if id := kcString(p, "id"); id != "" {
			parentID = id
			continue
		}
		name := kcString(p, "name")
		var children []map[string]any
		var err error
		if parentID == "" {
			children, err = kcListPath(ctx, conn, realm, "groups")
		} else {
			children, err = kcListPath(ctx, conn, realm, "groups/"+parentID+"/children")
		}
		if err != nil {
			return "", err
		}
		next := kcFindByField(children, "name", name)
		if next == nil {
			return "", nil
		}
		parentID = kcString(next, "id")
	}

	var siblings []map[string]any
	var err error
	if parentID == "" {
		siblings, err = kcListPath(ctx, conn, realm, "groups")
	} else {
		siblings, err = kcListPath(ctx, conn, realm, "groups/"+parentID+"/children")
	}
	if err != nil {
		return "", err
	}
	target := kcFindByField(siblings, "name", groupName)
	if target == nil {
		return "", nil
	}
	return kcString(target, "id"), nil
}
