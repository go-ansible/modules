package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClientscopeRolemappings implements Ansible's
// `keycloak_clientscope_rolemappings` (community.general) module: adds
// or removes realm/client roles from a client SCOPE's own role scope
// mappings, via kcadm.sh's own
// `client-scopes/<id>/scope-mappings/realm` (realm roles) or
// `client-scopes/<id>/scope-mappings/clients/<clientId>` (a client's
// own roles) resource paths — GET to read, POST/DELETE (both with a
// JSON array of role representations — see keycloak_json_helpers.go's
// own kcadmDeleteBody doc comment for the DELETE-with-body caveat) to
// mutate — verified against module_utils/_keycloak.py's own
// URL_CLIENTSCOPE_SCOPE_MAPPINGS_CLIENT/REALM.
//
// Args: clientscope_id (required — the client scope's own id or name;
// resolved by id first, falling back to a name lookup against the
// client-scopes collection since it has no server-side name filter —
// see keycloak_clientscope.go's own doc comment on the same
// constraint); client_id (optional — a client's own clientId that
// role_names' roles belong to; when omitted, role_names are REALM
// roles); realm (default master); role_names (required, list of role
// names); state (present|absent, default present).
//
// Idempotency: current scope roles are fetched, compared by name
// against role_names — only missing (state=present) or present
// (state=absent) roles are actually sent.
func moduleKeycloakClientscopeRolemappings(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_clientscope_rolemappings"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	clientscopeID, err := requireString(args, "clientscope_id")
	if err != nil {
		return Result{}, err
	}
	clientID := argString(args, "client_id", "")
	roleNames := argStringList(args, "role_names")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}

	csID, err := kcResolveClientScopeID(ctx, conn, realm, clientscopeID)
	if err != nil {
		return Result{}, err
	}
	if csID == "" {
		return Fail(fmt.Sprintf("%s: client scope %q not found in realm %s", mod, clientscopeID, realm)), nil
	}

	var path string
	var availableRoles []map[string]any
	if clientID != "" {
		cid, found, err := kcResolveClientID(ctx, conn, realm, clientID)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail(fmt.Sprintf("%s: client %q not found in realm %s", mod, clientID, realm)), nil
		}
		path = "client-scopes/" + csID + "/scope-mappings/clients/" + cid
		availableRoles, err = kcListPath(ctx, conn, realm, "clients/"+cid+"/roles")
		if err != nil {
			return Result{}, err
		}
	} else {
		path = "client-scopes/" + csID + "/scope-mappings/realm"
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
		if (state == "present" && !has) || (state == "absent" && has) {
			toApply = append(toApply, map[string]any{"id": kcString(role, "id"), "name": name})
		}
	}

	if len(toApply) == 0 {
		return Ok("Clientscope role scope is already up to date").WithExtra("end_state", current), nil
	}

	if state == "present" {
		res, err := kcadmCreateBody(ctx, conn, path, realm, toApply)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "add roles to clientscope role scope", res), nil
		}
	} else {
		res, err := kcadmDeleteBody(ctx, conn, path, realm, toApply)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "remove roles from clientscope role scope", res), nil
		}
	}

	final, err := kcListPath(ctx, conn, realm, path)
	if err != nil {
		return Result{}, err
	}
	return Changed("Clientscope role scope has been updated").WithExtra("end_state", final), nil
}

// kcResolveClientScopeID resolves idOrName to a client scope's own id:
// tried directly as an id first (a GET by id), falling back to a
// name lookup against the full client-scopes collection (which has no
// server-side name filter — see keycloak_clientscope.go's own doc
// comment). Returns "" (no error) if neither resolves.
func kcResolveClientScopeID(ctx context.Context, conn remoteexec.Connection, realm, idOrName string) (string, error) {
	if _, present, err := kcadmShow(ctx, conn, "client-scopes/"+idOrName, realm); err != nil {
		return "", err
	} else if present {
		return idOrName, nil
	}
	all, res, err := kcadmListMaps(ctx, conn, "client-scopes", realm, nil)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", fmt.Errorf("listing client scopes in realm %s: %s", realm, kcadmErrMsg(res))
	}
	if m := kcFindByField(all, "name", idOrName); m != nil {
		return kcString(m, "id"), nil
	}
	return "", nil
}
