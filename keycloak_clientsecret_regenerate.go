package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClientsecretRegenerate implements Ansible's
// `keycloak_clientsecret_regenerate` (community.general) module:
// regenerates a client's own secret, via kcadm.sh's own
// `clients/<id>/client-secret` resource path (POST, no request body —
// `kcadm.sh create clients/<id>/client-secret -r <realm>` with no
// `-s`/`-f` at all) — verified against module_utils/_keycloak.py's own
// URL_CLIENTSECRET and create_clientsecret (a POST with no data=
// argument at all).
//
// Args: realm (default master); id (the client's own internal id —
// skips a lookup when given) OR client_id (resolved via
// kcResolveClientID, costing one extra kcadm.sh invocation, matching
// real keycloak_clientsecret_regenerate.py's own doc). ALWAYS reports
// Changed=true (regenerating a secret is inherently a mutation —
// there is no "already regenerated" idempotent state), matching real
// keycloak_clientsecret_regenerate.py exactly.
//
// Security note carried over from the real module's own doc: this
// module's own return value contains the NEW client secret in plain
// text — a caller should set `no_log: true` on the task, exactly as
// real keycloak_clientsecret_regenerate.py's own doc recommends.
func moduleKeycloakClientsecretRegenerate(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_clientsecret_regenerate"
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

	res, err := kcadmCreate(ctx, conn, "clients/"+id+"/client-secret", realm, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf(mod, "regenerate client secret for "+id, res), nil
	}

	var secret map[string]any
	gres, err := kcadmGetJSON(ctx, conn, "clients/"+id+"/client-secret", realm, nil, &secret)
	if err != nil {
		return Result{}, err
	}
	if gres.RC != 0 {
		return kcadmFailedf(mod, "read regenerated client secret for "+id, gres), nil
	}
	return Changed(fmt.Sprintf("New client secret has been generated for ID %s", id)).WithExtra("end_state", secret), nil
}
