package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClientsecretInfo implements Ansible's
// `keycloak_clientsecret_info` (community.general) module: a read-only
// fetch of a client's own current secret, via kcadm.sh's own
// `clients/<id>/client-secret` resource path (GET) — verified against
// module_utils/_keycloak.py's own URL_CLIENTSECRET and
// get_clientsecret.
//
// Args: realm (default master); id (the client's own internal id —
// skips a lookup when given) OR client_id (the client's own clientId,
// resolved via kcResolveClientID — costs one extra kcadm.sh
// invocation, matching real keycloak_clientsecret_info.py's own
// documented "passing this instead of id results in an extra API
// call"). Never changes anything (Changed is always false).
//
// Security note carried over from the real module's own doc: this
// module's own return value contains the client secret in plain text
// — a caller should set `no_log: true` on the task to avoid it showing
// up in logs, exactly as real keycloak_clientsecret_info.py's own doc
// recommends.
func moduleKeycloakClientsecretInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_clientsecret_info"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	id, err := kcResolveClientsecretID(ctx, conn, mod, realm, args)
	if err != nil {
		return Result{}, err
	}
	if id == "" {
		return Fail(fmt.Sprintf("%s: client does not exist", mod)), nil
	}

	var secret map[string]any
	res, err := kcadmGetJSON(ctx, conn, "clients/"+id+"/client-secret", realm, nil, &secret)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf(mod, "get client secret for "+id, res), nil
	}
	return Ok(fmt.Sprintf("Get client secret successful for ID %s", id)).WithExtra("clientsecret_info", secret), nil
}

// kcResolveClientsecretID mirrors
// keycloak_clientsecret_module_resolve_params: prefers args["id"]
// directly (no lookup needed); falls back to resolving args["client_id"]
// via kcResolveClientID. Returns "" (no error) if neither resolves.
func kcResolveClientsecretID(ctx context.Context, conn remoteexec.Connection, mod, realm string, args map[string]any) (string, error) {
	if id := argString(args, "id", ""); id != "" {
		return id, nil
	}
	clientID, err := requireString(args, "client_id")
	if err != nil {
		return "", errArg("%s: one of id, client_id is required", mod)
	}
	id, found, err := kcResolveClientID(ctx, conn, realm, clientID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return id, nil
}
