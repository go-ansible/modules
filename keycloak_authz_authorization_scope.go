package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakAuthzAuthorizationScope implements Ansible's
// `keycloak_authz_authorization_scope` (community.general) module:
// manages an authorization scope belonging to a client that has
// Authorization Services enabled, via kcadm.sh's own
// `clients/<cid>/authz/resource-server/scope` (list/create) and
// `clients/<cid>/authz/resource-server/scope/<id>` (update/delete)
// resource paths, with lookup-by-name via that same collection's own
// `/search?name=` sub-endpoint (`-q name=<name>`) — verified against
// module_utils/_keycloak.py's own URL_AUTHZ_AUTHORIZATION_SCOPE(S) and
// get_authz_authorization_scope_by_name/create_authz_authorization_scope/
// update_authz_authorization_scope/remove_authz_authorization_scope.
// The real module's own doc notes the Authorization Services REST
// paths/payloads are not officially documented by the Keycloak project
// (citing a third-party blog post as the closest available reference);
// this port relies on the same module_utils source real
// keycloak_authz_authorization_scope.py itself relies on, not that
// blog post directly.
//
// Args: client_id (the client's own clientId, resolved to its internal
// UUID via kcResolveClientID); realm; name (the scope's own name,
// required); display_name; icon_uri; state (present|absent, default
// present).
//
// Idempotency: looked up by name; state=present creates if absent, or
// PUTs the full {name, displayName, iconUri, id} representation back
// (via kcMergeChangeset against the existing scope) only if
// display_name/icon_uri actually differ.
func moduleKeycloakAuthzAuthorizationScope(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_authz_authorization_scope"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm, err := requireString(args, "realm")
	if err != nil {
		return Result{}, err
	}
	clientID, err := requireString(args, "client_id")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}

	cid, found, err := kcResolveClientID(ctx, conn, realm, clientID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Fail(fmt.Sprintf("Invalid client %s for realm %s", clientID, realm)), nil
	}
	base := "clients/" + cid + "/authz/resource-server/scope"

	var existing map[string]any
	res, err := kcadmGetJSON(ctx, conn, base+"/search", realm, []string{"name=" + name}, &existing)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 || existing == nil || kcString(existing, "id") == "" {
		existing = nil
	}

	if state == "absent" {
		if existing == nil {
			return Ok(fmt.Sprintf("Authorization scope %s does not exist", name)), nil
		}
		dres, err := kcadmDelete(ctx, conn, base+"/"+kcString(existing, "id"), realm)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return kcadmFailedf(mod, "delete authorization scope "+name, dres), nil
		}
		return Changed(fmt.Sprintf("Authorization scope %s deleted", name)), nil
	}

	changeset := map[string]any{"name": name}
	kcSetIfPresent(changeset, args, "display_name", "displayName")
	kcSetIfPresent(changeset, args, "icon_uri", "iconUri")

	if existing == nil {
		cres, err := kcadmCreateBody(ctx, conn, base, realm, changeset)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return kcadmFailedf(mod, "create authorization scope "+name, cres), nil
		}
		var created map[string]any
		if gres, err := kcadmGetJSON(ctx, conn, base+"/search", realm, []string{"name=" + name}, &created); err != nil {
			return Result{}, err
		} else if gres.RC == 0 {
			existing = created
		}
		return Changed(fmt.Sprintf("Authorization scope %s created", name)).WithExtra("end_state", existing), nil
	}

	merged, changed := kcMergeChangeset(existing, changeset)
	if !changed {
		return Ok(fmt.Sprintf("Authorization scope %s already up to date", name)).WithExtra("end_state", existing), nil
	}
	ures, err := kcadmUpdateBody(ctx, conn, base+"/"+kcString(existing, "id"), realm, merged)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return kcadmFailedf(mod, "update authorization scope "+name, ures), nil
	}
	return Changed(fmt.Sprintf("Authorization scope %s updated", name)).WithExtra("end_state", merged), nil
}
