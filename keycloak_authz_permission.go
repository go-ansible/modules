package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakAuthzPermission implements Ansible's
// `keycloak_authz_permission` (community.general) module: manages a
// scope-based or resource-based authorization permission on a client
// that has Authorization Services enabled, via kcadm.sh's own
// `clients/<cid>/authz/resource-server/permission/<type>` (create) and
// `clients/<cid>/authz/resource-server/permission/<type>/<id>`
// (update) resource paths, DELETING via the generic
// `.../policy/<id>` path instead (Keycloak's own REST API deletes any
// authorization object — policy, scope-based permission, or
// resource-based permission — through the shared policy endpoint), and
// looking a permission up by name via
// `.../policy/search?name=<name>` — verified against
// module_utils/_keycloak.py's own URL_AUTHZ_PERMISSION(S)/
// get_authz_permission_by_name/create_authz_permission/
// update_authz_permission/remove_authz_permission: real
// keycloak_authz_permission.py's own module doc calls this exact
// create/update-via-permission-endpoint, get/delete-via-policy-endpoint
// split out explicitly as a Keycloak API peculiarity, not a mistake in
// this port.
//
// Args: client_id (resolved to its internal UUID); realm; name
// (required); permission_type (scope|resource, required);
// description; decision_strategy (UNANIMOUS|AFFIRMATIVE|CONSENSUS,
// default UNANIMOUS); resources (list of resource NAMEs, resolved to
// their own `_id` via a `.../resource/search?name=` lookup per
// element — scope-based permissions accept at most one);
// scopes (list of authorization-scope NAMEs, resolved via
// `.../scope/search?name=` — only valid for permission_type=scope, and
// (when a resource is also given) each must be one of that resource's
// own scopes, matching real keycloak_authz_permission.py's own
// validation exactly); policies (list of policy NAMEs, resolved via
// `.../policy/search?name=`); state (present|absent, default present).
//
// Deviation — always update, never diff, matching real
// keycloak_authz_permission.py's own module doc EXACTLY (quoted
// verbatim in its own source, since the doc explains WHY): "Updating
// an authorization permission is tricky... the current permission is
// retrieved using a _policy_ endpoint, not from a permission endpoint.
// Also, the data returned is in a different format than what is
// expected by the payload... there is no way to determine if any
// fields have changed. Therefore... a) Always apply the payload
// without checking the current state [is the approach taken]." This
// port does the exact same thing: an existing permission's own type
// cannot be changed (Fail(), matching real keycloak_authz_permission.py
// exactly) but every other update is applied UNCONDITIONALLY
// (Changed=true) whenever state=present and a permission with this
// name already exists — not a gap unique to this port, the real
// module has the identical limitation for the identical reason.
func moduleKeycloakAuthzPermission(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_authz_permission"
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
	permissionType, err := requireString(args, "permission_type")
	if err != nil {
		return Result{}, err
	}
	if permissionType != "scope" && permissionType != "resource" {
		return Result{}, errArg("%s: permission_type must be one of scope, resource, got %q", mod, permissionType)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	decisionStrategy := argString(args, "decision_strategy", "UNANIMOUS")
	resources := argStringList(args, "resources")
	scopes := argStringList(args, "scopes")
	policies := argStringList(args, "policies")

	if state == "present" {
		if permissionType == "scope" {
			if len(scopes) == 0 {
				return Fail("Scopes need to defined when permission type is set to scope!"), nil
			}
			if len(resources) > 1 {
				return Fail("Only one resource can be defined for a scope permission!"), nil
			}
		}
		if permissionType == "resource" {
			if len(resources) == 0 {
				return Fail("A resource need to defined when permission type is set to resource!"), nil
			}
			if len(scopes) > 0 {
				return Fail("Scopes cannot be defined when permission type is set to resource!"), nil
			}
		}
	}

	cid, found, err := kcResolveClientID(ctx, conn, realm, clientID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Fail(fmt.Sprintf("Invalid client %s for realm %s", clientID, realm)), nil
	}

	var existing map[string]any
	res, err := kcadmGetJSON(ctx, conn, "clients/"+cid+"/authz/resource-server/policy/search", realm, []string{"name=" + name}, &existing)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 || existing == nil || kcString(existing, "id") == "" {
		existing = nil
	}

	if existing == nil && state == "absent" {
		return Ok(fmt.Sprintf("Permission %s does not exist", name)), nil
	}
	if existing != nil && state == "absent" {
		dres, err := kcadmDelete(ctx, conn, "clients/"+cid+"/authz/resource-server/policy/"+kcString(existing, "id"), realm)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return kcadmFailedf(mod, "remove permission "+name, dres), nil
		}
		return Changed("Permission removed"), nil
	}

	payload := map[string]any{
		"name": name, "description": argString(args, "description", ""), "type": permissionType,
		"decisionStrategy": decisionStrategy, "logic": "POSITIVE",
		"scopes": []string{}, "resources": []string{}, "policies": []string{},
	}
	resourceIDs := []string{}
	if permissionType == "scope" {
		var resourceScopeIDs map[string]bool
		if len(resources) > 0 {
			r, found, err := kcAuthzFindByName(ctx, conn, realm, cid, "resource", resources[0])
			if err != nil {
				return Result{}, err
			}
			if !found {
				return Fail(fmt.Sprintf("Unable to find authorization resource with name %s for client %s in realm %s", resources[0], cid, realm)), nil
			}
			resourceIDs = append(resourceIDs, kcString(r, "_id"))
			resourceScopeIDs = map[string]bool{}
			for _, s := range asMapList(r["scopes"]) {
				resourceScopeIDs[kcString(s, "id")] = true
			}
		}
		scopeIDs := []string{}
		for _, scope := range scopes {
			s, found, err := kcAuthzFindByName(ctx, conn, realm, cid, "scope", scope)
			if err != nil {
				return Result{}, err
			}
			if !found {
				return Fail(fmt.Sprintf("%s: unable to find authorization scope with name %s for client %s in realm %s", mod, scope, cid, realm)), nil
			}
			if resourceScopeIDs != nil && !resourceScopeIDs[kcString(s, "id")] {
				return Fail(fmt.Sprintf("Resource %s does not include scope %s for client %s in realm %s", resources[0], scope, clientID, realm)), nil
			}
			scopeIDs = append(scopeIDs, kcString(s, "id"))
		}
		payload["scopes"] = scopeIDs
	} else {
		for _, resource := range resources {
			r, found, err := kcAuthzFindByName(ctx, conn, realm, cid, "resource", resource)
			if err != nil {
				return Result{}, err
			}
			if !found {
				return Fail(fmt.Sprintf("Unable to find authorization resource with name %s for client %s in realm %s", resource, cid, realm)), nil
			}
			resourceIDs = append(resourceIDs, kcString(r, "_id"))
		}
	}
	payload["resources"] = resourceIDs

	policyIDs := []string{}
	for _, policy := range policies {
		p, found, err := kcAuthzFindByName(ctx, conn, realm, cid, "policy", policy)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail(fmt.Sprintf("Unable to find authorization policy with name %s for client %s in realm %s", policy, clientID, realm)), nil
		}
		policyIDs = append(policyIDs, kcString(p, "id"))
	}
	payload["policies"] = policyIDs

	if existing != nil {
		payload["id"] = kcString(existing, "id")
		if kcString(existing, "type") != permissionType {
			return Fail(fmt.Sprintf("Modifying the type of permission (scope/resource) is not supported: "+
				"permission %s of client %s in realm %s unchanged", kcString(existing, "id"), cid, realm)), nil
		}
		ures, err := kcadmUpdateBody(ctx, conn, "clients/"+cid+"/authz/resource-server/permission/"+permissionType+"/"+kcString(existing, "id"), realm, payload)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return kcadmFailedf(mod, "update permission "+name, ures), nil
		}
		r := Changed("Notice: unable to check current resources, scopes and policies for permission. " +
			"Applying desired state without checking the current state.")
		return r.WithExtra("end_state", payload), nil
	}

	cres, err := kcadmCreateBody(ctx, conn, "clients/"+cid+"/authz/resource-server/permission/"+permissionType, realm, payload)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return kcadmFailedf(mod, "create permission "+name, cres), nil
	}
	return Changed("Permission created").WithExtra("end_state", payload), nil
}

// kcAuthzFindByName looks up one Authorization Services object
// (resource/scope/policy) by name via that collection's own
// `/search?name=` sub-endpoint, mirroring get_authz_resource_by_name/
// get_authz_authorization_scope_by_name/get_authz_policy_by_name —
// collection is "resource", "scope", or "policy" (the last segment of
// `clients/<cid>/authz/resource-server/<collection>`).
func kcAuthzFindByName(ctx context.Context, conn remoteexec.Connection, realm, cid, collection, name string) (map[string]any, bool, error) {
	var out map[string]any
	res, err := kcadmGetJSON(ctx, conn, "clients/"+cid+"/authz/resource-server/"+collection+"/search", realm, []string{"name=" + name}, &out)
	if err != nil {
		return nil, false, err
	}
	if res.RC != 0 || len(out) == 0 {
		return nil, false, nil
	}
	return out, true, nil
}
