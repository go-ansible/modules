package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakClient implements Ansible's `keycloak_client`
// (community.general) module: creates, updates, or deletes a Keycloak
// client (an OAuth2/OIDC or SAML application registration), via
// kcadm.sh's own `clients` (list/create) and `clients/<id>`
// (get/update/delete) resource paths — verified against
// module_utils/_keycloak.py's own URL_CLIENT(S).
//
// Args (real keycloak_client's own snake_case names, camelCase alias
// accepted too per real module doc — this port accepts both spellings
// the same way argStringAliased-style helpers elsewhere in this
// package do, via each arg's own explicit lookup below): realm
// (default master); id OR client_id (at least one required — id takes
// precedence, matching real module exactly); name; description;
// root_url; admin_url; base_url; surrogate_auth_required; enabled;
// client_authenticator_type (client-secret|client-jwt|client-x509);
// secret; registration_access_token; default_roles (list); redirect_uris
// (list); web_origins (list); valid_post_logout_redirect_uris (list —
// stored as attributes["post.logout.redirect.uris"], matching real
// module's own documented storage); not_before; bearer_only;
// consent_required; standard_flow_enabled; implicit_flow_enabled;
// direct_access_grants_enabled; service_accounts_enabled;
// authorization_services_enabled; public_client; frontchannel_logout;
// backchannel_logout_url (stored as
// attributes["backchannel.logout.url"]); protocol
// (openid-connect|saml|docker-v2, default openid-connect at creation
// only); attributes (dict, merged onto any attributes this port
// already set via backchannel_logout_url/valid_post_logout_redirect_uris
// above); full_scope_allowed; node_re_registration_timeout;
// registered_nodes (dict); client_template; use_template_config;
// use_template_scope; use_template_mappers; always_display_in_console;
// authentication_flow_binding_overrides (dict: browser/browser_name/
// direct_grant/direct_grant_name — a `_name` variant is resolved to
// its flow's own id via kcFindFlowByAlias, mirroring real
// flow_binding_from_dict_to_model); protocol_mappers (list of dicts);
// authorization_settings (dict); client_scopes_behavior
// (ignore|patch|idempotent, default ignore) plus default_client_scopes/
// optional_client_scopes (lists) — see below; state (present|absent,
// default present).
//
// Idempotency: existing client fetched by id (if given) or by
// client_id (via kcResolveClientID); every given field above is merged
// onto it (kcMergeChangeset) and PUT back only if something actually
// differs. protocol defaults to openid-connect only when CREATING (no
// existing client to merge onto).
//
// Deviation — attributes/authenticationFlowBindingOverrides merge:
// real keycloak_client.py's own merge_settings_without_absent_nulls
// merges a nested dict field key-by-key, PRESERVING any existing key
// not mentioned in the desired value and treating an explicit null in
// the desired value as "remove this key" — a partial-merge semantic.
// This port instead replaces the WHOLE attributes (respectively
// authenticationFlowBindingOverrides) field with the merged result of
// existing-attributes-shallow-copy plus every attribute this port
// itself sets — simpler, and equivalent for every documented use case
// in this batch's own examples, but not a byte-for-byte port of the
// real per-key null-removal semantics.
//
// Deviation — client_scopes_behavior: this port implements "patch"
// (add any of default_client_scopes/optional_client_scopes not already
// assigned, via a PUT to clients/<id>/default-client-scopes/<scopeId>
// or .../optional-client-scopes/<scopeId> — no body, an add-only join)
// and "idempotent" (patch, PLUS remove any CURRENTLY assigned scope of
// that type not in the desired list, via DELETE on the same path) both
// faithfully; "ignore" (the default) never touches client scopes at
// all, matching real keycloak_client.py exactly.
func moduleKeycloakClient(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_client"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	id := argString(args, "id", "")
	clientID := argString(args, "client_id", "")
	if id == "" && clientID == "" {
		return Result{}, errArg("%s: one of id, client_id is required", mod)
	}

	var existing map[string]any
	var found bool
	var err error
	if id != "" {
		existing, found, err = kcadmShow(ctx, conn, "clients/"+id, realm)
	} else {
		var resolvedID string
		resolvedID, found, err = kcResolveClientID(ctx, conn, realm, clientID)
		if err == nil && found {
			id = resolvedID
			existing, found, err = kcadmShow(ctx, conn, "clients/"+id, realm)
		}
	}
	if err != nil {
		return Result{}, err
	}
	if existing == nil {
		existing = map[string]any{}
	}

	if !found {
		if state == "absent" {
			return Ok("Client does not exist; doing nothing."), nil
		}
		if clientID == "" {
			return Result{}, errArg("%s: client_id needs to be specified when creating a new client", mod)
		}
		changeset, err := kcClientChangeset(ctx, conn, realm, args, existing)
		if err != nil {
			return Result{}, err
		}
		changeset["clientId"] = clientID
		if _, ok := changeset["protocol"]; !ok {
			changeset["protocol"] = "openid-connect"
		}
		res, err := kcadmCreateBody(ctx, conn, "clients", realm, changeset)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "create client "+clientID, res), nil
		}
		newID, found, err := kcResolveClientID(ctx, conn, realm, clientID)
		if err != nil {
			return Result{}, err
		}
		var after map[string]any
		if found {
			after, _, err = kcadmShow(ctx, conn, "clients/"+newID, realm)
			if err != nil {
				return Result{}, err
			}
		}
		if err := kcApplyClientScopes(ctx, conn, realm, newID, args, map[string]any{}); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("Client %s has been created.", clientID)).WithExtra("end_state", after), nil
	}

	if state == "absent" {
		res, err := kcadmDelete(ctx, conn, "clients/"+id, realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "delete client "+id, res), nil
		}
		return Changed(fmt.Sprintf("Client %s has been deleted.", kcString(existing, "clientId"))), nil
	}

	changeset, err := kcClientChangeset(ctx, conn, realm, args, existing)
	if err != nil {
		return Result{}, err
	}
	merged, changed := kcMergeChangeset(existing, changeset)
	scopesChanged, err := kcClientScopesWouldChange(ctx, conn, realm, id, args)
	if err != nil {
		return Result{}, err
	}
	if !changed && !scopesChanged {
		return Ok(fmt.Sprintf("No changes required for Client %s.", kcString(existing, "clientId"))).
			WithExtra("end_state", existing), nil
	}

	if changed {
		res, err := kcadmUpdateBody(ctx, conn, "clients/"+id, realm, merged)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "update client "+id, res), nil
		}
	}
	if err := kcApplyClientScopes(ctx, conn, realm, id, args, existing); err != nil {
		return Result{}, err
	}
	after, _, err := kcadmShow(ctx, conn, "clients/"+id, realm)
	if err != nil {
		return Result{}, err
	}
	return Changed(fmt.Sprintf("Client %s has been updated.", kcString(existing, "clientId"))).
		WithExtra("end_state", after), nil
}

// kcClientChangeset builds the client representation changeset from
// args, merging backchannel_logout_url/valid_post_logout_redirect_uris
// into an attributes sub-changeset per real keycloak_client.py's own
// documented attribute storage (see moduleKeycloakClient's own doc
// comment).
func kcClientChangeset(ctx context.Context, conn remoteexec.Connection, realm string, args map[string]any, existing map[string]any) (map[string]any, error) {
	c := map[string]any{}
	kcSetIfPresent(c, args, "id", "id")
	kcSetIfPresent(c, args, "name", "name")
	kcSetIfPresent(c, args, "description", "description")
	kcSetIfPresent(c, args, "root_url", "rootUrl")
	kcSetIfPresent(c, args, "admin_url", "adminUrl")
	kcSetIfPresent(c, args, "base_url", "baseUrl")
	kcSetIfPresent(c, args, "surrogate_auth_required", "surrogateAuthRequired")
	kcSetIfPresent(c, args, "enabled", "enabled")
	kcSetIfPresent(c, args, "client_authenticator_type", "clientAuthenticatorType")
	kcSetIfPresent(c, args, "secret", "secret")
	kcSetIfPresent(c, args, "registration_access_token", "registrationAccessToken")
	kcSetIfPresent(c, args, "default_roles", "defaultRoles")
	kcSetIfPresent(c, args, "redirect_uris", "redirectUris")
	kcSetIfPresent(c, args, "web_origins", "webOrigins")
	kcSetIfPresent(c, args, "not_before", "notBefore")
	kcSetIfPresent(c, args, "bearer_only", "bearerOnly")
	kcSetIfPresent(c, args, "consent_required", "consentRequired")
	kcSetIfPresent(c, args, "standard_flow_enabled", "standardFlowEnabled")
	kcSetIfPresent(c, args, "implicit_flow_enabled", "implicitFlowEnabled")
	kcSetIfPresent(c, args, "direct_access_grants_enabled", "directAccessGrantsEnabled")
	kcSetIfPresent(c, args, "service_accounts_enabled", "serviceAccountsEnabled")
	kcSetIfPresent(c, args, "authorization_services_enabled", "authorizationServicesEnabled")
	kcSetIfPresent(c, args, "public_client", "publicClient")
	kcSetIfPresent(c, args, "frontchannel_logout", "frontchannelLogout")
	kcSetIfPresent(c, args, "protocol", "protocol")
	kcSetIfPresent(c, args, "full_scope_allowed", "fullScopeAllowed")
	kcSetIfPresent(c, args, "node_re_registration_timeout", "nodeReRegistrationTimeout")
	kcSetIfPresent(c, args, "registered_nodes", "registeredNodes")
	kcSetIfPresent(c, args, "client_template", "clientTemplate")
	kcSetIfPresent(c, args, "use_template_config", "useTemplateConfig")
	kcSetIfPresent(c, args, "use_template_scope", "useTemplateScope")
	kcSetIfPresent(c, args, "use_template_mappers", "useTemplateMappers")
	kcSetIfPresent(c, args, "always_display_in_console", "alwaysDisplayInConsole")
	kcSetIfPresent(c, args, "protocol_mappers", "protocolMappers")
	kcSetIfPresent(c, args, "authorization_settings", "authorizationSettings")

	attrs := map[string]any{}
	if existingAttrs, ok := existing["attributes"].(map[string]any); ok {
		for k, v := range existingAttrs {
			attrs[k] = v
		}
	}
	haveAttrOverride := false
	if v, ok := args["backchannel_logout_url"]; ok {
		attrs["backchannel.logout.url"] = v
		haveAttrOverride = true
	}
	if v, ok := args["valid_post_logout_redirect_uris"]; ok {
		attrs["post.logout.redirect.uris"] = v
		haveAttrOverride = true
	}
	if given := argMapAny(args, "attributes"); given != nil {
		for k, v := range given {
			attrs[k] = v
		}
		haveAttrOverride = true
	}
	if haveAttrOverride {
		c["attributes"] = attrs
	}

	if fb := argMapAny(args, "authentication_flow_binding_overrides"); fb != nil {
		overrides, err := kcResolveFlowBindingOverrides(ctx, conn, realm, fb)
		if err != nil {
			return nil, err
		}
		c["authenticationFlowBindingOverrides"] = overrides
	}
	return c, nil
}

// kcResolveFlowBindingOverrides mirrors flow_binding_from_dict_to_model:
// browser/direct_grant are already flow IDs and pass through as-is;
// browser_name/direct_grant_name are resolved to their flow's own id
// via kcFindFlowByAlias (reused from keycloak_authentication_v2.go,
// same package).
func kcResolveFlowBindingOverrides(ctx context.Context, conn remoteexec.Connection, realm string, fb map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if v, ok := fb["browser"]; ok {
		out["browser"] = v
	} else if name, ok := fb["browser_name"].(string); ok && name != "" {
		flow, found, err := kcFindFlowByAlias(ctx, conn, realm, name)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("authentication_flow_binding_overrides.browser_name: flow %q not found in realm %s", name, realm)
		}
		out["browser"] = kcString(flow, "id")
	}
	if v, ok := fb["direct_grant"]; ok {
		out["direct_grant"] = v
	} else if name, ok := fb["direct_grant_name"].(string); ok && name != "" {
		flow, found, err := kcFindFlowByAlias(ctx, conn, realm, name)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("authentication_flow_binding_overrides.direct_grant_name: flow %q not found in realm %s", name, realm)
		}
		out["direct_grant"] = kcString(flow, "id")
	}
	return out, nil
}

// kcApplyClientScopes implements moduleKeycloakClient's own
// client_scopes_behavior handling (patch/idempotent; ignore is a
// no-op) for both default_client_scopes and optional_client_scopes.
func kcApplyClientScopes(ctx context.Context, conn remoteexec.Connection, realm, clientUUID string, args map[string]any, existing map[string]any) error {
	behavior := argString(args, "client_scopes_behavior", "ignore")
	if behavior == "ignore" || clientUUID == "" {
		return nil
	}
	if desired, ok := args["default_client_scopes"]; ok {
		if err := kcReconcileClientScopeType(ctx, conn, realm, clientUUID, "default-client-scopes", argStringList(map[string]any{"v": desired}, "v"), behavior); err != nil {
			return err
		}
	}
	if desired, ok := args["optional_client_scopes"]; ok {
		if err := kcReconcileClientScopeType(ctx, conn, realm, clientUUID, "optional-client-scopes", argStringList(map[string]any{"v": desired}, "v"), behavior); err != nil {
			return err
		}
	}
	return nil
}

// kcClientScopesWouldChange reports whether kcApplyClientScopes would
// actually add or remove anything, so an unrelated re-run with
// client_scopes_behavior set does not report Changed=true when nothing
// would move.
func kcClientScopesWouldChange(ctx context.Context, conn remoteexec.Connection, realm, clientUUID string, args map[string]any) (bool, error) {
	behavior := argString(args, "client_scopes_behavior", "ignore")
	if behavior == "ignore" {
		return false, nil
	}
	for _, pair := range []struct {
		argKey, path string
	}{{"default_client_scopes", "default-client-scopes"}, {"optional_client_scopes", "optional-client-scopes"}} {
		desiredRaw, ok := args[pair.argKey]
		if !ok {
			continue
		}
		desired := argStringList(map[string]any{"v": desiredRaw}, "v")
		current, res, err := kcadmListMaps(ctx, conn, "clients/"+clientUUID+"/"+pair.path, realm, nil)
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, fmt.Errorf("listing %s for client %s: %s", pair.path, clientUUID, kcadmErrMsg(res))
		}
		currentNames := map[string]bool{}
		for _, s := range current {
			currentNames[kcString(s, "name")] = true
		}
		desiredNames := map[string]bool{}
		for _, d := range desired {
			desiredNames[d] = true
			if !currentNames[d] {
				return true, nil
			}
		}
		if behavior == "idempotent" {
			for n := range currentNames {
				if !desiredNames[n] {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// kcReconcileClientScopeType adds (and, when behavior=="idempotent",
// removes) client-level default/optional client scopes to match
// desired — path is "default-client-scopes" or
// "optional-client-scopes" (relative to clients/<clientUUID>/).
func kcReconcileClientScopeType(ctx context.Context, conn remoteexec.Connection, realm, clientUUID, path string, desired []string, behavior string) error {
	allScopes, res, err := kcadmListMaps(ctx, conn, "client-scopes", realm, nil)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("listing client-scopes in realm %s: %s", realm, kcadmErrMsg(res))
	}
	current, res, err := kcadmListMaps(ctx, conn, "clients/"+clientUUID+"/"+path, realm, nil)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("listing %s for client %s: %s", path, clientUUID, kcadmErrMsg(res))
	}
	currentByName := map[string]string{}
	for _, s := range current {
		currentByName[kcString(s, "name")] = kcString(s, "id")
	}
	desiredSet := map[string]bool{}
	for _, name := range desired {
		desiredSet[name] = true
		if _, already := currentByName[name]; already {
			continue
		}
		scope := kcFindByField(allScopes, "name", name)
		if scope == nil {
			return fmt.Errorf("client scope %q not found in realm %s", name, realm)
		}
		if res, err := kcadmUpdate(ctx, conn, "clients/"+clientUUID+"/"+path+"/"+kcString(scope, "id"), realm, nil, nil); err != nil {
			return err
		} else if res.RC != 0 {
			return fmt.Errorf("adding client scope %q to client %s: %s", name, clientUUID, kcadmErrMsg(res))
		}
	}
	if behavior == "idempotent" {
		for name, id := range currentByName {
			if desiredSet[name] {
				continue
			}
			if res, err := kcadmDelete(ctx, conn, "clients/"+clientUUID+"/"+path+"/"+id, realm); err != nil {
				return err
			} else if res.RC != 0 {
				return fmt.Errorf("removing client scope %q from client %s: %s", name, clientUUID, kcadmErrMsg(res))
			}
		}
	}
	return nil
}
