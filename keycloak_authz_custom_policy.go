package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakAuthzCustomPolicy implements Ansible's
// `keycloak_authz_custom_policy` (community.general) module: manages a
// custom (JAR-deployed) JavaScript authorization policy on a client
// that has Authorization Services enabled, via kcadm.sh's own
// `clients/<cid>/authz/resource-server/policy/<policy_type>` (create)
// and `clients/<cid>/authz/resource-server/policy/<id>` (delete)
// resource paths, with lookup-by-name via
// `clients/<cid>/authz/resource-server/policy/search?name=` (`-q
// name=<name>`) — verified against module_utils/_keycloak.py's own
// URL_AUTHZ_CUSTOM_POLICY(IES)/get_authz_policy_by_name/
// create_authz_custom_policy/remove_authz_custom_policy.
//
// Args: client_id (resolved to its internal UUID via
// kcResolveClientID); realm; name (required); policy_type (required —
// must match the name of the custom policy already deployed to the
// server as a JAR); state (present|absent, default present).
//
// Deviation — no update path, matching real
// keycloak_authz_custom_policy.py exactly (its own module doc
// comment: "Modifying existing custom policies is not possible"; its
// own diff_mode support is documented as "none"): if a policy with
// this name already exists, state=present is ALWAYS a no-op
// (Changed=false) regardless of whether policy_type differs from what
// was requested — this port does not attempt an update Keycloak's own
// API has no endpoint for either.
func moduleKeycloakAuthzCustomPolicy(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_authz_custom_policy"
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

	var existing map[string]any
	res, err := kcadmGetJSON(ctx, conn, "clients/"+cid+"/authz/resource-server/policy/search", realm, []string{"name=" + name}, &existing)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 || existing == nil || kcString(existing, "id") == "" {
		existing = nil
	}

	desired := map[string]any{"name": name, "type": argString(args, "policy_type", "")}

	if existing != nil && state == "present" {
		return Ok(fmt.Sprintf("Custom policy %s already exists", name)).WithExtra("end_state", desired), nil
	}
	if existing == nil && state == "present" {
		policyType, err := requireString(args, "policy_type")
		if err != nil {
			return Result{}, err
		}
		cres, err := kcadmCreateBody(ctx, conn, "clients/"+cid+"/authz/resource-server/policy/"+policyType, realm, desired)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return kcadmFailedf(mod, "create custom policy "+name, cres), nil
		}
		return Changed(fmt.Sprintf("Custom policy %s created", name)).WithExtra("end_state", desired), nil
	}
	if existing != nil && state == "absent" {
		dres, err := kcadmDelete(ctx, conn, "clients/"+cid+"/authz/resource-server/policy/"+kcString(existing, "id"), realm)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return kcadmFailedf(mod, "delete custom policy "+name, dres), nil
		}
		return Changed(fmt.Sprintf("Custom policy %s removed", name)), nil
	}
	// existing == nil && state == absent
	return Ok(fmt.Sprintf("Custom policy %s does not exist", name)), nil
}
