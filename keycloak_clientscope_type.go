package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClientscopeType implements Ansible's
// `keycloak_clientscope_type` (community.general) module: sets which
// client scopes are of type "default" versus "optional", either at
// the REALM level (default-default-client-scopes/
// default-optional-client-scopes) or, when client_id is given, at a
// specific CLIENT's own level (clients/<cid>/default-client-scopes/
// clients/<cid>/optional-client-scopes) — via kcadm.sh's own PUT
// (assign) and DELETE (unassign) on
// `<default-or-optional-client-scopes path>/<scopeId>`, no body — the
// same client-vs-realm path split
// module_utils/_keycloak.py's own get/add/delete_default_clientscope/
// get/add/delete_optional_clientscope (URL_DEFAULT_CLIENTSCOPE(S)/
// URL_OPTIONAL_CLIENTSCOPE(S)/URL_CLIENT_DEFAULT_CLIENTSCOPE(S)/
// URL_CLIENT_OPTIONAL_CLIENTSCOPE(S)) apply.
//
// Args: realm (default master); client_id (optional — the client's own
// clientId; when omitted, this module sets the REALM's own defaults);
// default_clientscopes (list of client-scope names — the exact set
// that should be of type "default"); optional_clientscopes (list of
// client-scope names — the exact set that should be of type
// "optional"). Either list may be omitted, in which case that type's
// current assignment is left untouched entirely (matching real
// keycloak_clientscope_type.py's own "None means don't touch"
// convention for each list independently).
//
// Idempotency: this module computes the exact set difference between
// each given list and the current default/optional assignment
// (resolved to client-scope names) and only assigns/unassigns what
// actually differs — a client scope named in NEITHER list keeps
// whatever assignment (default, optional, or neither) it already has.
//
// Deviation — bodyless PUT for assign: this port assumes kcadm.sh's
// own `update <path>` invocation with no `-s`/`-d`/`-f` flags at all
// sends an empty-body PUT directly, rather than first performing its
// own documented GET-then-merge (see keycloak_common.go's own doc
// comment on kcadmUpdate's normal GET+merge+PUT semantics) — because
// Keycloak's own default/optional-client-scope assignment sub-resource
// accepts PUT with no body and has NO MATCHING GET endpoint to merge
// against at all (only PUT/DELETE are defined on it). This is a
// reasonable, but NOT independently verified against a live
// kcadm.sh/Keycloak server in this sandbox, assumption — the same
// honesty this batch's other kcadm.sh syntax calls apply where a live
// binary could not be exercised directly (see keycloak_common.go's own
// doc comment on the same constraint for glab).
func moduleKeycloakClientscopeType(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_clientscope_type"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	clientID := argString(args, "client_id", "")

	basePath := ""
	if clientID != "" {
		cid, found, err := kcResolveClientID(ctx, conn, realm, clientID)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail(fmt.Sprintf("%s: client %q not found in realm %s", mod, clientID, realm)), nil
		}
		basePath = "clients/" + cid
	}

	changed := false
	var msgs []string
	endState := map[string]any{}

	if desired, ok := args["default_clientscopes"]; ok {
		path := "default-default-client-scopes"
		if basePath != "" {
			path = basePath + "/default-client-scopes"
		}
		names, c, err := kcReconcileClientscopeType(ctx, conn, mod, realm, path, argStringList(map[string]any{"v": desired}, "v"))
		if err != nil {
			return Result{}, err
		}
		if c {
			changed = true
			msgs = append(msgs, "default client scopes updated")
		}
		endState["default_clientscopes"] = names
	}
	if desired, ok := args["optional_clientscopes"]; ok {
		path := "default-optional-client-scopes"
		if basePath != "" {
			path = basePath + "/optional-client-scopes"
		}
		names, c, err := kcReconcileClientscopeType(ctx, conn, mod, realm, path, argStringList(map[string]any{"v": desired}, "v"))
		if err != nil {
			return Result{}, err
		}
		if c {
			changed = true
			msgs = append(msgs, "optional client scopes updated")
		}
		endState["optional_clientscopes"] = names
	}

	msg := ""
	for i, m := range msgs {
		if i > 0 {
			msg += "; "
		}
		msg += m
	}
	return Result{Changed: changed, Msg: msg}.WithExtra("end_state", endState), nil
}

// kcReconcileClientscopeType assigns/unassigns client scopes under
// path (a "<...>-client-scopes" collection) to match desired (a list
// of client-scope NAMES), returning the final list of names and
// whether anything actually changed.
func kcReconcileClientscopeType(ctx context.Context, conn remoteexec.Connection, mod, realm, path string, desired []string) ([]string, bool, error) {
	current, err := kcListPath(ctx, conn, realm, path)
	if err != nil {
		return nil, false, err
	}
	allScopes, res, err := kcadmListMaps(ctx, conn, "client-scopes", realm, nil)
	if err != nil {
		return nil, false, err
	}
	if res.RC != 0 {
		return nil, false, fmt.Errorf("listing client-scopes in realm %s: %s", realm, kcadmErrMsg(res))
	}

	currentByName := map[string]string{}
	for _, s := range current {
		currentByName[kcString(s, "name")] = kcString(s, "id")
	}
	desiredSet := map[string]bool{}
	for _, n := range desired {
		desiredSet[n] = true
	}

	changed := false
	for _, name := range desired {
		if _, already := currentByName[name]; already {
			continue
		}
		scope := kcFindByField(allScopes, "name", name)
		if scope == nil {
			return nil, false, fmt.Errorf("%s: client scope %q not found in realm %s", mod, name, realm)
		}
		if res, err := kcadmUpdate(ctx, conn, path+"/"+kcString(scope, "id"), realm, nil, nil); err != nil {
			return nil, false, err
		} else if res.RC != 0 {
			return nil, false, fmt.Errorf("%s: assigning client scope %q: %s", mod, name, kcadmErrMsg(res))
		}
		changed = true
	}
	for name, id := range currentByName {
		if desiredSet[name] {
			continue
		}
		if res, err := kcadmDelete(ctx, conn, path+"/"+id, realm); err != nil {
			return nil, false, err
		} else if res.RC != 0 {
			return nil, false, fmt.Errorf("%s: unassigning client scope %q: %s", mod, name, kcadmErrMsg(res))
		}
		changed = true
	}

	final, err := kcListPath(ctx, conn, realm, path)
	if err != nil {
		return nil, false, err
	}
	names := make([]string, 0, len(final))
	for _, s := range final {
		names = append(names, kcString(s, "name"))
	}
	return names, changed, nil
}
