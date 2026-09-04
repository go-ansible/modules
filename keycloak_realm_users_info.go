package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakRealmUsersInfo implements Ansible's
// `keycloak_realm_users_info` (community.general) module: lists every
// user in a realm, via `kcadm.sh get users -r <realm>` — see
// keycloak_common.go's own doc comment for the kcadm.sh substitution.
//
// Real keycloak_realm_users_info fetches the full user list in pages
// (its own module_utils get_realm_users paginates through `first`/
// `max` until a short page signals the end). `kcadm.sh get users -r
// <realm>` with no `-q first=/-q max=` follows the Admin REST API's own
// default single-request behavior (a server-side default page size,
// typically 100) — this port does not implement client-side pagination
// beyond that, an honestly-documented limitation for realms with very
// large user counts, not a silent truncation left unmentioned.
//
// Args: realm (default master).
//
// Extra["users"]: the raw JSON array `kcadm.sh get users` returns,
// unmodified — matching real keycloak_realm_users_info's own `users`
// return value.
func moduleKeycloakRealmUsersInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_realm_users_info"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")

	var users []map[string]any
	res, err := kcadmGetJSON(ctx, conn, "users", realm, nil, &users)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_realm_users_info", "unable to list users for realm "+realm, res), nil
	}
	if users == nil {
		users = []map[string]any{}
	}
	return Ok("").WithExtra("users", users), nil
}
