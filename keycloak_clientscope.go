package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClientscope implements Ansible's `keycloak_clientscope`
// (community.general) module: manages a Keycloak client scope, via
// kcadm.sh's own `client-scopes` (list/create) and `client-scopes/<id>`
// (get/update/delete) resource paths — verified against
// module_utils/_keycloak.py's own URL_CLIENTSCOPE(S). The client-scopes
// collection has no server-side name filter (real
// get_clientscope_by_name's own doc: "The Keycloak API does not allow
// filtering of the clientscopes resource by name... first retrieves
// the entire list... then performs a second query"), so lookup-by-name
// lists the full collection and matches client-side.
//
// Args: realm (default master); id (a UUID) OR name — at least one
// needed to look an existing scope up, name required to create one;
// description; protocol (openid-connect|saml|wsfed|docker-v2);
// protocol_mappers (list of dicts); protocol_mappers_behavior
// (exact|subset, default subset — see below); attributes (dict, values
// may be a single scalar or a list — both accepted as-is, matching
// real keycloak_clientscope.py's own documented "translated into a
// list suitable for the API" behavior loosely: this port passes the
// value straight through to kcadm.sh's own JSON body rather than
// normalizing scalars to a one-element list itself, since the
// Keycloak API accepts a scalar there too); state (present|absent,
// default present).
//
// Idempotency: looked up by id/name; kcMergeChangeset against the
// existing representation decides whether an update is needed.
//
// Deviation — protocol_mappers_behavior: this port applies it the same
// way client_scopes_behavior's "patch"/exact distinction works
// elsewhere in this batch (see keycloak_client.go's own doc comment):
// "subset" (the default) merges protocol_mappers into whatever
// protocolMappers the existing scope already has (matched by name,
// missing ones added, no existing one removed or altered) before
// diffing/updating; "exact" replaces protocolMappers with exactly the
// given list. Real keycloak_clientscope.py's own semantics are
// documented identically ("subset": add missing, don't remove;
// "exact": make it exactly as specified).
func moduleKeycloakClientscope(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_clientscope"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	id := argString(args, "id", "")
	name := argString(args, "name", "")
	if id == "" && name == "" {
		return Result{}, errArg("%s: one of id, name is required", mod)
	}

	all, res, err := kcadmListMaps(ctx, conn, "client-scopes", realm, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf(mod, "list client scopes in realm "+realm, res), nil
	}
	var existing map[string]any
	if id != "" {
		existing = kcFindByField(all, "id", id)
	} else {
		existing = kcFindByField(all, "name", name)
	}

	if existing == nil {
		if state == "absent" {
			return Ok("Client_scope does not exist; doing nothing."), nil
		}
		if name == "" {
			return Result{}, errArg("%s: name needs to be specified when creating a new client scope", mod)
		}
		changeset := kcClientscopeChangeset(args, nil)
		changeset["name"] = name
		cres, err := kcadmCreateBody(ctx, conn, "client-scopes", realm, changeset)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return kcadmFailedf(mod, "create client scope "+name, cres), nil
		}
		all2, _, err := kcadmListMaps(ctx, conn, "client-scopes", realm, nil)
		if err != nil {
			return Result{}, err
		}
		after := kcFindByField(all2, "name", name)
		return Changed(fmt.Sprintf("Client_scope %s has been created.", name)).WithExtra("end_state", after), nil
	}

	if state == "absent" {
		dres, err := kcadmDelete(ctx, conn, "client-scopes/"+kcString(existing, "id"), realm)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return kcadmFailedf(mod, "delete client scope "+kcString(existing, "name"), dres), nil
		}
		return Changed(fmt.Sprintf("Client_scope %s has been deleted.", kcString(existing, "name"))), nil
	}

	changeset := kcClientscopeChangeset(args, existing)
	merged, changed := kcMergeChangeset(existing, changeset)
	if !changed {
		return Ok(fmt.Sprintf("No changes required for client_scope %s.", kcString(existing, "name"))).
			WithExtra("end_state", existing), nil
	}
	ures, err := kcadmUpdateBody(ctx, conn, "client-scopes/"+kcString(existing, "id"), realm, merged)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return kcadmFailedf(mod, "update client scope "+kcString(existing, "name"), ures), nil
	}
	after, _, err := kcadmShow(ctx, conn, "client-scopes/"+kcString(existing, "id"), realm)
	if err != nil {
		return Result{}, err
	}
	return Changed(fmt.Sprintf("Client_scope %s has been updated.", kcString(existing, "name"))).
		WithExtra("end_state", after), nil
}

func kcClientscopeChangeset(args map[string]any, existing map[string]any) map[string]any {
	c := map[string]any{}
	kcSetIfPresent(c, args, "description", "description")
	kcSetIfPresent(c, args, "protocol", "protocol")
	kcSetIfPresent(c, args, "attributes", "attributes")

	if mappers := argListOfMaps(args, "protocol_mappers"); mappers != nil {
		behavior := argString(args, "protocol_mappers_behavior", "subset")
		if behavior == "exact" || existing == nil {
			c["protocolMappers"] = mappers
		} else {
			existingMappers := asMapList(existing["protocolMappers"])
			byName := map[string]bool{}
			for _, m := range existingMappers {
				byName[kcString(m, "name")] = true
			}
			merged := append([]map[string]any{}, existingMappers...)
			for _, m := range mappers {
				if !byName[kcString(m, "name")] {
					merged = append(merged, m)
				}
			}
			c["protocolMappers"] = merged
		}
	}
	return c
}
