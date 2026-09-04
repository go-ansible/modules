package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakAuthzPermissionInfo implements Ansible's
// `keycloak_authz_permission_info` (community.general) module: a
// read-only lookup of one authorization permission by name, via
// kcadm.sh's own `clients/<cid>/authz/resource-server/policy/search?
// name=<name>` resource path (`-q name=<name>`) — the SAME
// policy-endpoint lookup keycloak_authz_permission.go's own kcadm
// calls use (see that file's own doc comment on why permissions are
// read via the policy endpoint, not a permission-specific one) —
// verified against module_utils/_keycloak.py's own
// get_authz_permission_by_name.
//
// Args: client_id (resolved to its internal UUID); realm; name
// (required). Never changes anything (Changed is always false).
func moduleKeycloakAuthzPermissionInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_authz_permission_info"
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

	cid, found, err := kcResolveClientID(ctx, conn, realm, clientID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Fail(fmt.Sprintf("Invalid client %s for realm %s", clientID, realm)), nil
	}

	permission, found, err := kcAuthzFindByName(ctx, conn, realm, cid, "policy", name)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Fail(fmt.Sprintf("Unable to find authorization permission with name %s for client %s in realm %s", name, clientID, realm)), nil
	}
	return Ok(fmt.Sprintf("Get permission %s successful", name)).WithExtra("queried_state", permission), nil
}
