package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakRealmKeysMetadataInfo implements Ansible's
// `keycloak_realm_keys_metadata_info` (community.general) module:
// fetches a realm's keys metadata (KeysMetadataRepresentation), via
// `kcadm.sh get keys -r <realm>` — the Admin REST API's own
// `GET /admin/realms/{realm}/keys` endpoint, module_utils' own
// URL_REALM_KEYS_METADATA — see keycloak_common.go's own doc comment
// for the kcadm.sh substitution.
//
// Args: realm (default master).
//
// Extra["keys_metadata"]: the raw JSON object `kcadm.sh get keys`
// returns, unmodified — matching real
// keycloak_realm_keys_metadata_info's own `keys_metadata` return value
// exactly (an `active` map of algorithm -> key UUID, plus a `keys` list
// of per-key detail).
func moduleKeycloakRealmKeysMetadataInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_realm_keys_metadata_info"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")

	var metadata map[string]any
	res, err := kcadmGetJSON(ctx, conn, "keys", realm, nil, &metadata)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_realm_keys_metadata_info", "unable to fetch keys metadata for realm "+realm, res), nil
	}
	return Ok("").WithExtra("keys_metadata", metadata), nil
}
