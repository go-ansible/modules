package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClienttemplate implements Ansible's
// `keycloak_clienttemplate` (community.general) module: manages a
// legacy Keycloak client template (superseded in modern Keycloak by
// client scopes — real keycloak_clienttemplate.py's own module doc
// still ships it for older-server compatibility), via kcadm.sh's own
// `client-templates` (list/create) and `client-templates/<id>`
// (get/update/delete) resource paths — verified against
// module_utils/_keycloak.py's own URL_CLIENTTEMPLATE(S). The
// client-templates collection has no server-side name filter (like
// client-scopes), so lookup-by-name lists the full collection and
// matches client-side, mirroring get_client_template_by_name's own
// loop.
//
// Args: realm (default master); id (a UUID) OR name — at least one
// needed to look an existing template up, name required to create one;
// description; protocol (openid-connect|saml|docker-v2);
// full_scope_allowed; attributes (dict); protocol_mappers (list of
// dicts); state (present|absent, default present).
//
// Deviation, matching real keycloak_clienttemplate.py's own documented
// NOTE exactly: bearerOnly/consentRequired/standardFlowEnabled/
// implicitFlowEnabled/directAccessGrantsEnabled/
// serviceAccountsEnabled/publicClient/frontchannelLogout are all real
// keycloak_client fields the Keycloak REST API silently discards on a
// client-template request — this module has no such arguments at all,
// matching the real module's own argument spec exactly (not a gap:
// there is nothing to accept).
func moduleKeycloakClienttemplate(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_clienttemplate"
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

	all, res, err := kcadmListMaps(ctx, conn, "client-templates", realm, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf(mod, "list client templates in realm "+realm, res), nil
	}
	var existing map[string]any
	if id != "" {
		existing = kcFindByField(all, "id", id)
	} else {
		existing = kcFindByField(all, "name", name)
	}

	if existing == nil {
		if state == "absent" {
			return Ok("Client template does not exist; doing nothing."), nil
		}
		if name == "" {
			return Result{}, errArg("%s: name needs to be specified when creating a new client template", mod)
		}
		changeset := kcClienttemplateChangeset(args)
		changeset["name"] = name
		cres, err := kcadmCreateBody(ctx, conn, "client-templates", realm, changeset)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return kcadmFailedf(mod, "create client template "+name, cres), nil
		}
		all2, _, err := kcadmListMaps(ctx, conn, "client-templates", realm, nil)
		if err != nil {
			return Result{}, err
		}
		after := kcFindByField(all2, "name", name)
		return Changed(fmt.Sprintf("Client template %s has been created.", name)).WithExtra("end_state", after), nil
	}

	if state == "absent" {
		dres, err := kcadmDelete(ctx, conn, "client-templates/"+kcString(existing, "id"), realm)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return kcadmFailedf(mod, "delete client template "+kcString(existing, "name"), dres), nil
		}
		return Changed(fmt.Sprintf("Client template %s has been deleted.", kcString(existing, "name"))), nil
	}

	changeset := kcClienttemplateChangeset(args)
	merged, changed := kcMergeChangeset(existing, changeset)
	if !changed {
		return Ok(fmt.Sprintf("No changes required for client template %s.", kcString(existing, "name"))).
			WithExtra("end_state", existing), nil
	}
	ures, err := kcadmUpdateBody(ctx, conn, "client-templates/"+kcString(existing, "id"), realm, merged)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return kcadmFailedf(mod, "update client template "+kcString(existing, "name"), ures), nil
	}
	after, _, err := kcadmShow(ctx, conn, "client-templates/"+kcString(existing, "id"), realm)
	if err != nil {
		return Result{}, err
	}
	return Changed(fmt.Sprintf("Client template %s has been updated.", kcString(existing, "name"))).
		WithExtra("end_state", after), nil
}

func kcClienttemplateChangeset(args map[string]any) map[string]any {
	c := map[string]any{}
	kcSetIfPresent(c, args, "description", "description")
	kcSetIfPresent(c, args, "protocol", "protocol")
	kcSetIfPresent(c, args, "full_scope_allowed", "fullScopeAllowed")
	kcSetIfPresent(c, args, "attributes", "attributes")
	kcSetIfPresent(c, args, "protocol_mappers", "protocolMappers")
	return c
}
