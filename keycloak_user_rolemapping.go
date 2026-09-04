package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakUserRolemapping implements Ansible's
// `keycloak_user_rolemapping` (community.general) module: maps or
// unmaps roles onto a user, via `kcadm.sh add-roles --uid <id>
// [--cclientid <client-uuid>] --rolename <r> ...`/`remove-roles`/
// `get-roles` — see keycloak_common.go's own doc comment for the
// kcadm.sh substitution and its role-mapping convenience commands, and
// keycloak_realm_rolemapping.go's own doc comment for why `roles` is
// mapped by NAME only (never by `roles[].id` alone).
//
// Args: uid — when given, used directly; target_username (one extra
// `kcadm.sh get users -q username=` lookup) otherwise; cid — when
// given, used directly as the client's own internal UUID; client_id (a
// clientId, resolved to its UUID via `kcadm.sh get clients -q
// clientId=`) otherwise; service_account_user_client_id — resolves to
// that client's own service-account user via Keycloak's own documented
// `service-account-<clientId>` username convention (verified against
// Keycloak's own service-account-user naming scheme, not guessed);
// neither cid/client_id/service_account_user_client_id set maps a REALM
// role instead (matching real keycloak_user_rolemapping's own
// documented "If neither cid nor client_id is specified, a realm role
// is mapped instead"); roles; state (present|absent, default present).
//
// Like keycloak_realm_rolemapping, `roles` is add/remove-only, not a
// full-replace set: state=present adds every listed role not already
// mapped; state=absent removes every listed role that IS currently
// mapped.
func moduleKeycloakUserRolemapping(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_user_rolemapping"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_user_rolemapping: state must be one of present, absent, got %q", state)
	}

	uid := argString(args, "uid", "")
	if uid == "" {
		username := argString(args, "target_username", "")
		if username == "" {
			svcClient := argString(args, "service_account_user_client_id", "")
			if svcClient != "" {
				username = "service-account-" + svcClient
			}
		}
		if username == "" {
			return Result{}, errArg("keycloak_user_rolemapping: one of uid, target_username, service_account_user_client_id is required")
		}
		resolved, found, err := kcadmFindUserByUsername(ctx, conn, realm, username)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail("keycloak_user_rolemapping: no such user: " + username), nil
		}
		uid = resolved
	}

	cid := argString(args, "cid", "")
	if cid == "" {
		if clientID := argString(args, "client_id", ""); clientID != "" {
			resolved, found, err := kcadmFindClientByClientID(ctx, conn, realm, clientID)
			if err != nil {
				return Result{}, err
			}
			if !found {
				return Fail("keycloak_user_rolemapping: no such client: " + clientID), nil
			}
			cid = resolved
		}
	}

	rolesRaw, _ := args["roles"].([]any)
	var desired []string
	for _, r := range rolesRaw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		n := argString(m, "name", "")
		if n == "" {
			return Result{}, errArg("keycloak_user_rolemapping: each entry of roles must have a name (this port maps roles by name, not id)")
		}
		desired = append(desired, n)
	}
	if len(desired) == 0 {
		return Result{}, errArg("keycloak_user_rolemapping: roles must not be empty")
	}

	target := kcadmRoleTarget{flag: "--uid", value: uid, clientID: cid}
	current, res, err := kcadmGetRoles(ctx, conn, realm, target)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_user_rolemapping", "unable to list current role mappings for user "+uid, res), nil
	}
	currentSet := map[string]bool{}
	for _, r := range current {
		currentSet[r.Name] = true
	}

	if state == "absent" {
		var toRemove []string
		for _, n := range desired {
			if currentSet[n] {
				toRemove = append(toRemove, n)
			}
		}
		if len(toRemove) == 0 {
			return Ok("No roles to unmap from user " + uid), nil
		}
		res, err := kcadmRemoveRoles(ctx, conn, realm, target, toRemove)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_user_rolemapping", "unable to unmap roles from user "+uid, res), nil
		}
		return Changed("Roles unmapped from user " + uid), nil
	}

	var toAdd []string
	for _, n := range desired {
		if !currentSet[n] {
			toAdd = append(toAdd, n)
		}
	}
	if len(toAdd) == 0 {
		return Ok("User " + uid + " already has the requested role mappings"), nil
	}
	res, err = kcadmAddRoles(ctx, conn, realm, target, toAdd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_user_rolemapping", "unable to map roles to user "+uid, res), nil
	}
	return Changed("Roles mapped to user " + uid), nil
}
