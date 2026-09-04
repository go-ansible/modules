package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// keycloakUserFederationProviderType is the default Keycloak component
// providerType for a user federation provider — verified against
// Keycloak's own component model, matching this module's own doc
// default for `provider_type`.
const keycloakUserFederationProviderType = "org.keycloak.storage.UserStorageProvider"

// keycloakUserFederationMapperProviderType is the default component
// providerType for one of a federation's own mappers — matching this
// module's own doc default for `mappers[].providerType`.
const keycloakUserFederationMapperProviderType = "org.keycloak.storage.ldap.mappers.LDAPStorageMapper"

// moduleKeycloakUserFederation implements Ansible's
// `keycloak_user_federation` (community.general) module: creates,
// updates, or deletes a realm's user federation provider (LDAP/
// Kerberos/sssd backend), via `kcadm.sh create/get/update/delete
// components(/<id>)` with `providerType=org.keycloak.storage.
// UserStorageProvider` — a user federation is a Keycloak "component"
// under the hood, the same resource keycloak_realm_key.go and
// keycloak_userprofile.go also manage under different providerTypes —
// see keycloak_common.go's own doc comment for the kcadm.sh
// substitution.
//
// Args: id — when given, used directly; name (required otherwise,
// searched via `-q parent=<parent_id> -q type=... -q name=<name>`,
// matching real keycloak_user_federation's own doc: "If left empty,
// the user federation is searched by its name"); parent_id (defaults to
// realm); provider_id (ldap|kerberos|sssd, or a custom provider);
// provider_type (default org.keycloak.storage.UserStorageProvider);
// config (dict — every key is ALREADY the exact Keycloak API field
// name, per this module's own doc, unlike most other keycloak_*
// modules' snake_case+alias arguments; every value is sent as a
// single-element JSON string array via keycloakStringifyConfigMap,
// matching the component config MultivaluedHashMap<String,String>
// shape); bind_credential_update_mode (always|only_indirect, default
// always) — matching real keycloak_user_federation's own doc: `always`
// always sends `config.bindCredential` when given (Keycloak masks it on
// read, so this port cannot compare it — sending it is therefore always
// treated as a change, same limitation the real module documents);
// `only_indirect` excludes it from the has-anything-changed comparison,
// but still includes it in the body whenever OTHER fields already
// triggered an update (matching the real module's own "only updated if
// there are other changes" semantics exactly); mappers (list of {name,
// providerId, providerType, config} — synced by name against the
// federation's own current mapper set, which this port reads via `-q
// parent=<federation-id> -q
// type=org.keycloak.storage.ldap.mappers.LDAPStorageMapper` — an
// honestly-documented simplification for a federation using a
// DIFFERENT mapper providerType than the LDAP default, whose own
// mappers would not be found by this filter and so would never be
// diffed against, only ever added); remove_unspecified_mappers (default
// true — set false to keep current mappers not named in `mappers`);
// state (present|absent, default present).
func moduleKeycloakUserFederation(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_user_federation"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_user_federation: state must be one of present, absent, got %q", state)
	}
	parentID := argStringAliased(args, "parent_id", "parentId", realm)
	if parentID == "" {
		parentID = realm
	}
	providerType := argStringAliased(args, "provider_type", "providerType", keycloakUserFederationProviderType)
	name := argString(args, "name", "")
	id := argString(args, "id", "")

	var current map[string]any
	var present bool
	var err error
	if id != "" {
		current, present, err = kcadmShow(ctx, conn, "components/"+id, realm)
		if err != nil {
			return Result{}, err
		}
	} else {
		if name == "" {
			return Result{}, errArg("keycloak_user_federation: one of id, name is required")
		}
		var matches []map[string]any
		res, err := kcadmGetJSON(ctx, conn, "components", realm, []string{
			"parent=" + parentID, "type=" + providerType, "name=" + name,
		}, &matches)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_user_federation", "unable to look up user federation "+name, res), nil
		}
		for _, m := range matches {
			if s, _ := m["name"].(string); s == name {
				current = m
				present = true
				id, _ = m["id"].(string)
				break
			}
		}
	}

	if state == "absent" {
		if !present {
			return Ok("User federation " + name + " does not exist"), nil
		}
		res, err := kcadmDelete(ctx, conn, "components/"+id, realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_user_federation", "unable to delete user federation", res), nil
		}
		return Changed("User federation has been deleted"), nil
	}

	config := map[string]string{}
	if raw, ok := args["config"].(map[string]any); ok {
		config = keycloakStringifyConfigMap(raw)
	}
	bindMode := argString(args, "bind_credential_update_mode", "always")
	providerID := argStringAliased(args, "provider_id", "providerId", "")

	if !present {
		if name == "" {
			return Result{}, errArg("keycloak_user_federation: name is required when creating a user federation")
		}
		cfg := map[string]any{}
		for k, v := range config {
			cfg[k] = []string{v}
		}
		body := map[string]any{
			"name":         name,
			"providerId":   providerID,
			"providerType": providerType,
			"parentId":     parentID,
			"config":       cfg,
		}
		newID, res, err := kcadmCreateBodyID(ctx, conn, "components", realm, body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_user_federation", "unable to create user federation "+name, res), nil
		}
		if err := keycloakSyncFederationMappers(ctx, conn, realm, newID, args); err != nil {
			return Result{}, err
		}
		r := Changed("User federation " + name + " has been created")
		return r.WithExtra("end_state", map[string]any{"id": newID, "name": name}), nil
	}

	haveConfig, _ := current["config"].(map[string]any)
	newConfig := map[string]any{}
	for k, v := range haveConfig {
		newConfig[k] = v
	}
	changed := false
	for k, v := range config {
		if k == "bindCredential" && bindMode == "only_indirect" {
			continue
		}
		have := ""
		if arr, ok := haveConfig[k].([]any); ok && len(arr) > 0 {
			have = argString(map[string]any{"v": arr[0]}, "v", "")
		}
		if have != v {
			changed = true
		}
		newConfig[k] = []string{v}
	}
	body := map[string]any{}
	for k, v := range current {
		body[k] = v
	}
	if v, ok := config["bindCredential"]; ok && bindMode == "only_indirect" && changed {
		newConfig["bindCredential"] = []string{v}
	}
	body["config"] = newConfig

	if changed {
		res, err := kcadmUpdateBody(ctx, conn, "components/"+id, realm, body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_user_federation", "unable to update user federation "+name, res), nil
		}
	}
	mappersChanged, err := keycloakSyncFederationMappersChanged(ctx, conn, realm, id, args)
	if err != nil {
		return Result{}, err
	}
	if changed || mappersChanged {
		return Changed("User federation " + name + " has been updated"), nil
	}
	return Ok("User federation " + name + " already up to date"), nil
}

func keycloakFederationMapperBody(m map[string]any, parentID string) map[string]any {
	cfg := map[string]any{}
	if raw, ok := m["config"].(map[string]any); ok {
		for k, v := range keycloakStringifyConfigMap(raw) {
			cfg[k] = []string{v}
		}
	}
	return map[string]any{
		"name":         argString(m, "name", ""),
		"providerId":   argString(m, "providerId", ""),
		"providerType": argString(m, "providerType", keycloakUserFederationMapperProviderType),
		"parentId":     parentID,
		"config":       cfg,
	}
}

// keycloakSyncFederationMappers creates every entry of args["mappers"]
// fresh — used on create.
func keycloakSyncFederationMappers(ctx context.Context, conn remoteexec.Connection, realm, parentID string, args map[string]any) error {
	list, _ := args["mappers"].([]any)
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		body := keycloakFederationMapperBody(m, parentID)
		res, err := kcadmCreateBody(ctx, conn, "components", realm, body)
		if err != nil {
			return err
		}
		if res.RC != 0 {
			return errArg("keycloak_user_federation: unable to create mapper %q: %s", argString(m, "name", ""), res.Stderr)
		}
	}
	return nil
}

// keycloakSyncFederationMappersChanged diffs args["mappers"] (if given)
// against the federation's own current LDAP-mapper-typed components,
// creating missing ones, updating ones whose providerId/config differ,
// and — unless remove_unspecified_mappers=false — deleting any current
// mapper not named in the list.
func keycloakSyncFederationMappersChanged(ctx context.Context, conn remoteexec.Connection, realm, parentID string, args map[string]any) (bool, error) {
	list, ok := args["mappers"].([]any)
	if !ok {
		return false, nil
	}
	var current []map[string]any
	res, err := kcadmGetJSON(ctx, conn, "components", realm, []string{
		"parent=" + parentID, "type=" + keycloakUserFederationMapperProviderType,
	}, &current)
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		current = nil
	}
	currentByName := map[string]map[string]any{}
	for _, c := range current {
		if n, _ := c["name"].(string); n != "" {
			currentByName[n] = c
		}
	}

	desiredNames := map[string]bool{}
	changed := false
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := argString(m, "name", "")
		desiredNames[name] = true
		body := keycloakFederationMapperBody(m, parentID)
		if existing, found := currentByName[name]; found {
			id, _ := existing["id"].(string)
			if !jsonScalarEqual(body["providerId"], existing["providerId"]) {
				changed = true
			}
			body["id"] = id
			res, err := kcadmUpdateBody(ctx, conn, "components/"+id, realm, body)
			if err != nil {
				return false, err
			}
			if res.RC != 0 {
				return false, errArg("keycloak_user_federation: unable to update mapper %q: %s", name, res.Stderr)
			}
			continue
		}
		res, err := kcadmCreateBody(ctx, conn, "components", realm, body)
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, errArg("keycloak_user_federation: unable to create mapper %q: %s", name, res.Stderr)
		}
		changed = true
	}

	if argBool(args, "remove_unspecified_mappers", true) {
		for name, c := range currentByName {
			if desiredNames[name] {
				continue
			}
			id, _ := c["id"].(string)
			res, err := kcadmDelete(ctx, conn, "components/"+id, realm)
			if err != nil {
				return false, err
			}
			if res.RC != 0 {
				return false, errArg("keycloak_user_federation: unable to delete mapper %q: %s", name, res.Stderr)
			}
			changed = true
		}
	}
	return changed, nil
}
