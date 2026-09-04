package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakComponentInfo implements Ansible's
// `keycloak_component_info` (community.general) module: retrieves
// realm component representations, via `kcadm.sh get components -r
// <realm> -q parent=<parent> [-q name=<name>] [-q type=<provider_type>]`
// — see keycloak_common.go's own doc comment for the kcadm.sh
// substitution and its accepted-but-inert auth_*/token/
// validate_certs/connection_timeout/http_agent arguments.
//
// Args: realm (required); name; parent_id (defaults to realm — real
// keycloak_component_info first resolves the realm's own internal
// `id` via a separate realm lookup and defaults `parent` to THAT; this
// port defaults directly to the realm NAME instead, since Keycloak
// itself sets a realm's own `id` equal to its `realm` name by default
// and this port has no cheaper way to confirm that without an extra
// round trip — an honestly-documented simplification, not a silent
// behavior change, that only matters for the rare realm whose internal
// id has been deliberately changed from its name); provider_type.
//
// Extra["components"]: the raw JSON array `kcadm.sh get components`
// returns, unmodified — matching real keycloak_component_info's own
// `components` return value exactly (a list of Component
// representations).
func moduleKeycloakComponentInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_component_info"); !ok {
		return res, nil
	}
	realm, err := requireString(args, "realm")
	if err != nil {
		return Result{}, err
	}

	parentID := argString(args, "parent_id", "")
	if parentID == "" {
		parentID = realm
	}
	query := []string{"parent=" + parentID}
	if name := argString(args, "name", ""); name != "" {
		query = append(query, "name="+name)
	}
	if pt := argString(args, "provider_type", ""); pt != "" {
		query = append(query, "type="+pt)
	}

	var components []map[string]any
	res, err := kcadmGetJSON(ctx, conn, "components", realm, query, &components)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_component_info", "unable to list components", res), nil
	}
	if components == nil {
		components = []map[string]any{}
	}
	return Ok("").WithExtra("components", components), nil
}
