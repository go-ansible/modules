package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClientRolescope implements Ansible's
// `keycloak_client_rolescope` (community.general) module: adds or
// removes realm/client roles from a client's own "full scope
// disabled" role scope, via kcadm.sh's own
// `clients/<id>/scope-mappings/realm` (realm roles) or
// `clients/<id>/scope-mappings/clients/<scopeId>` (another client's
// own roles) resource paths — GET to read, POST (a JSON array of role
// representations) to add, DELETE (also a JSON array body — see
// keycloak_json_helpers.go's own kcadmDeleteBody doc comment) to
// remove — verified against module_utils/_keycloak.py's own
// URL_CLIENT_ROLE_SCOPE_CLIENTS/REALM and
// get/update/delete_client_role_scope_from_client/realm.
//
// Args: client_id (required — the client whose scope is being
// managed); client_scope_id (optional — another client's own clientId
// that role_names' roles belong to; when omitted, role_names are
// REALM roles); realm (default master); role_names (required, list of
// role names); state (present|absent, default present).
//
// Real keycloak_client_rolescope.py's own doc requires client_id's own
// full_scope_allowed to already be false (set via keycloak_client's own
// full_scope_allowed=false) — this port does the same pre-check real
// keycloak_client_rolescope.py itself performs (state=present only,
// matching the real module exactly: a state=absent removal is allowed
// even with fullScopeAllowed=true, since it can't make anything worse).
//
// Idempotency: current scope roles are fetched, compared by name
// against role_names — only missing (state=present) or present
// (state=absent) roles are actually sent.
func moduleKeycloakClientRolescope(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_client_rolescope"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	clientID, err := requireString(args, "client_id")
	if err != nil {
		return Result{}, err
	}
	clientScopeID := argString(args, "client_scope_id", "")
	roleNames := argStringList(args, "role_names")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}

	cid, found, err := kcResolveClientID(ctx, conn, realm, clientID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Fail(fmt.Sprintf("Failed to retrieve client '%s.%s'", realm, clientID)), nil
	}
	client, present, err := kcadmShow(ctx, conn, "clients/"+cid, realm)
	if err != nil {
		return Result{}, err
	}
	if !present {
		return Fail(fmt.Sprintf("Failed to retrieve client '%s.%s'", realm, clientID)), nil
	}
	if state == "present" {
		if b, _ := client["fullScopeAllowed"].(bool); b {
			return Fail(fmt.Sprintf("FullScopeAllowed is active for Client '%s.%s'", realm, clientID)), nil
		}
	}

	var path string
	var availableRoles []map[string]any
	if clientScopeID != "" {
		scopeCID, found, err := kcResolveClientID(ctx, conn, realm, clientScopeID)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail(fmt.Sprintf("Failed to retrieve client '%s.%s'", realm, clientScopeID)), nil
		}
		path = "clients/" + cid + "/scope-mappings/clients/" + scopeCID
		availableRoles, err = kcListPath(ctx, conn, realm, "clients/"+scopeCID+"/roles")
		if err != nil {
			return Result{}, err
		}
	} else {
		path = "clients/" + cid + "/scope-mappings/realm"
		availableRoles, err = kcListPath(ctx, conn, realm, "roles")
		if err != nil {
			return Result{}, err
		}
	}

	current, err := kcListPath(ctx, conn, realm, path)
	if err != nil {
		return Result{}, err
	}
	currentByName := map[string]bool{}
	for _, r := range current {
		currentByName[kcString(r, "name")] = true
	}

	var toApply []map[string]any
	for _, name := range roleNames {
		role := kcFindByField(availableRoles, "name", name)
		if role == nil {
			return Fail(fmt.Sprintf("%s: role %q not found", mod, name)), nil
		}
		has := currentByName[name]
		if state == "present" && !has {
			toApply = append(toApply, map[string]any{"id": kcString(role, "id"), "name": name})
		}
		if state == "absent" && has {
			toApply = append(toApply, map[string]any{"id": kcString(role, "id"), "name": name})
		}
	}

	if len(toApply) == 0 {
		return Ok("Client role scope for "+clientID+" is already up to date").WithExtra("end_state", current), nil
	}

	if state == "present" {
		res, err := kcadmCreateBody(ctx, conn, path, realm, toApply)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "add roles to client role scope for "+clientID, res), nil
		}
	} else {
		res, err := kcadmDeleteBody(ctx, conn, path, realm, toApply)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "remove roles from client role scope for "+clientID, res), nil
		}
	}

	final, err := kcListPath(ctx, conn, realm, path)
	if err != nil {
		return Result{}, err
	}
	return Changed(fmt.Sprintf("Client role scope for %s has been updated", clientID)).WithExtra("end_state", final), nil
}

// kcListPath is kcadmListMaps with no query, for a caller that already
// has a full resource path (not a resource-name + query pair).
func kcListPath(ctx context.Context, conn remoteexec.Connection, realm, path string) ([]map[string]any, error) {
	list, res, err := kcadmListMaps(ctx, conn, path, realm, nil)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("listing %s: %s", path, kcadmErrMsg(res))
	}
	return list, nil
}
