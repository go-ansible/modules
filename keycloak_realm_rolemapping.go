package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakRealmRolemapping implements Ansible's
// `keycloak_realm_rolemapping` (community.general) module: maps or
// unmaps realm-level roles onto a group, via `kcadm.sh add-roles
// --gid <id> --rolename <r> ...`/`remove-roles`/`get-roles` (no
// `--cclientid`, which is what selects REALM-level roles instead of one
// client's own roles — see keycloak_common.go's own doc comment for the
// kcadm.sh substitution and its role-mapping convenience commands).
//
// Args: gid — when given, used directly; group_name (+ optional
// parents, resolved exactly like keycloak_group.go's own
// keycloakResolveGroupPath) otherwise; roles (list of {id, name} — this
// port always maps by NAME, since kcadm's own add-roles/remove-roles/
// get-roles convenience commands take `--rolename`, not a role ID; a
// `roles[].id`-only entry with no `name` is a Result{Failed:true}, an
// honestly-documented gap rather than a silent no-op — real
// keycloak_realm_rolemapping accepts id-only entries because its own
// REST client can address a role purely by ID); state (present|absent,
// default present).
//
// Unlike keycloak_group's own attribute handling, `roles` here is NOT a
// full-replace set: state=present adds every listed role not already
// mapped (existing mappings not listed are left alone); state=absent
// removes every listed role that IS currently mapped — matching real
// keycloak_realm_rolemapping's own documented "map/unmap the given
// roles" semantics (there is no `purge`-style option on this module,
// unlike gitlab_project_members's own purge_users).
func moduleKeycloakRealmRolemapping(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_realm_rolemapping"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_realm_rolemapping: state must be one of present, absent, got %q", state)
	}

	gid := argString(args, "gid", "")
	if gid == "" {
		groupName := argString(args, "group_name", "")
		if groupName == "" {
			return Result{}, errArg("keycloak_realm_rolemapping: one of gid, group_name is required")
		}
		resolvedID, _, found, err := keycloakResolveGroupPath(ctx, conn, realm, args, groupName)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail("keycloak_realm_rolemapping: no such group: " + groupName), nil
		}
		gid = resolvedID
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
			return Result{}, errArg("keycloak_realm_rolemapping: each entry of roles must have a name (this port maps roles by name, not id)")
		}
		desired = append(desired, n)
	}
	if len(desired) == 0 {
		return Result{}, errArg("keycloak_realm_rolemapping: roles must not be empty")
	}

	target := kcadmRoleTarget{flag: "--gid", value: gid}
	current, res, err := kcadmGetRoles(ctx, conn, realm, target)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_realm_rolemapping", "unable to list current role mappings for group "+gid, res), nil
	}
	var currentNames []string
	for _, r := range current {
		currentNames = append(currentNames, r.Name)
	}

	currentSet := map[string]bool{}
	for _, c := range currentNames {
		currentSet[c] = true
	}

	if state == "absent" {
		var toRemove []string
		for _, n := range desired {
			if currentSet[n] {
				toRemove = append(toRemove, n)
			}
		}
		if len(toRemove) == 0 {
			return Ok("No roles to unmap from group " + gid), nil
		}
		res, err := kcadmRemoveRoles(ctx, conn, realm, target, toRemove)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_realm_rolemapping", "unable to unmap roles from group "+gid, res), nil
		}
		return Changed("Roles unmapped from group " + gid), nil
	}

	var toAdd []string
	for _, n := range desired {
		found := false
		for _, c := range currentNames {
			if c == n {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, n)
		}
	}
	if len(toAdd) == 0 {
		return Ok("Group " + gid + " already has the requested realm role mappings"), nil
	}
	res, err = kcadmAddRoles(ctx, conn, realm, target, toAdd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_realm_rolemapping", "unable to map roles to group "+gid, res), nil
	}
	return Changed("Roles mapped to group " + gid), nil
}
