package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakRealmInfo implements Ansible's `keycloak_realm_info`
// (community.general) module: fetches a realm's public information —
// see keycloak_common.go's own doc comment for the kcadm.sh
// substitution.
//
// Real keycloak_realm_info calls Keycloak's own PUBLIC, non-admin
// endpoint (`{server}/realms/{realm}`, module_utils' own
// URL_REALM_INFO — deliberately NOT under `/admin/realms/`, so it needs
// no admin credentials at all). `kcadm.sh` is an admin-only CLI: every
// command it runs is scoped under `/admin/realms/{realm}/...` and it
// has no invocation mode for the separate public realm-info endpoint.
// This is a genuine, honestly-documented architecture gap, not a
// guess: this module instead runs `kcadm.sh get realms/<realm>` (the
// ADMIN realm representation) and maps what it can from that instead:
//   - `realm` -> realm_info.realm (same field, same value).
//   - `public_key` -> realm_info.public_key, from the admin
//     representation's own deprecated (but still populated by
//     Keycloak for backward compatibility) top-level `publicKey`
//     field, which mirrors the currently active RS256 signing key —
//     the same value the public endpoint's own `public_key` reports.
//   - `notBefore` -> realm_info.tokens-not-before (same underlying
//     "revoke tokens issued before this Unix time" value, just named
//     differently between the two representations).
//   - `account-service`/`token-service` (the public endpoint's own
//     account-console and OIDC-issuer URLs) are NOT populated: both
//     are derived from the auth server's own base URL, which this
//     port treats as inert (see keycloak_common.go's own doc comment
//     on auth_keycloak_url) — fabricating them from a guess would be
//     silently wrong, so this module leaves both as empty strings
//     rather than inventing a URL.
//
// Args: realm (default master); auth_keycloak_url — accepted, no
// effect (see keycloak_common.go).
func moduleKeycloakRealmInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_realm_info"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")

	admin, present, err := kcadmShow(ctx, conn, "realms/"+realm, "")
	if err != nil {
		return Result{}, err
	}
	if !present {
		return Fail("keycloak_realm_info: realm " + realm + " not found"), nil
	}

	info := map[string]any{
		"realm":             admin["realm"],
		"public_key":        admin["publicKey"],
		"tokens-not-before": admin["notBefore"],
		"account-service":   "",
		"token-service":     "",
	}
	return Ok("").WithExtra("realm_info", info), nil
}
