package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// keycloakIDPField maps one top-level keycloak_identity_provider
// scalar argument to its identically-spelled Keycloak
// IdentityProviderRepresentation field — taken directly from each
// option's own documented `aliases` entry in `ansible-doc
// community.general.keycloak_identity_provider` (the camelCase alias
// IS the real API field name, per this module's own doc: "aliases are
// provided so camelCased versions can be used as well"), not guessed.
var keycloakIDPFields = []struct {
	arg, api, kind string
}{
	{"add_read_token_role_on_create", "addReadTokenRoleOnCreate", "bool"},
	{"authenticate_by_default", "authenticateByDefault", "bool"},
	{"display_name", "displayName", "string"},
	{"enabled", "enabled", "bool"},
	{"first_broker_login_flow_alias", "firstBrokerLoginFlowAlias", "string"},
	{"link_only", "linkOnly", "bool"},
	{"post_broker_login_flow_alias", "postBrokerLoginFlowAlias", "string"},
	{"store_token", "storeToken", "bool"},
	{"trust_email", "trustEmail", "bool"},
}

// moduleKeycloakIdentityProvider implements Ansible's
// `keycloak_identity_provider` (community.general) module: creates,
// updates, or deletes a realm's external identity provider (SAML/OIDC
// federation), via `kcadm.sh create/get/update/delete
// identity-provider/instances/<alias>` — Keycloak's own Admin REST API
// path for this resource, addressed by `alias` directly (no internal
// UUID the way users/groups/components are) — see keycloak_common.go's
// own doc comment for the kcadm.sh substitution.
//
// Args: alias (required); realm (default master); provider_id (used
// only at creation — real keycloak_identity_provider also treats it as
// immutable after creation, since the underlying protocol implementation
// cannot be swapped in place); config (dict — every
// IdentityProviderRepresentation.config value is itself a STRING in the
// real API regardless of the YAML type given, so this port stringifies
// every config value via keycloakStringifyConfigMap before sending it,
// matching real keycloak_identity_provider's own equivalent
// coercion); mappers (list of {name, identityProviderMapper, config} —
// synced by name: a desired mapper missing on the server is created, one
// present with a different providerId/config is updated, and — ONLY
// when `mappers` is explicitly given — any CURRENT mapper not named in
// the list is deleted, a full-replace convention matching this port's
// own gitlab_project.go topics field rather than an additive-only one);
// plus every keycloakIDPFields entry above; state (present|absent,
// default present).
//
// Deviation from real keycloak_identity_provider: `config.clientSecret`
// is masked by Keycloak on every GET (`**********`), so this port
// cannot compare a desired secret against the current one — matching
// real keycloak_identity_provider's own documented inability to do so,
// this port always INCLUDES `config.clientSecret` in the update body
// whenever the task provides one, accepting a redundant update rather
// than silently never applying a rotated secret.
func moduleKeycloakIdentityProvider(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_identity_provider"); !ok {
		return res, nil
	}
	alias, err := requireString(args, "alias")
	if err != nil {
		return Result{}, err
	}
	realm := argString(args, "realm", "master")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_identity_provider: state must be one of present, absent, got %q", state)
	}
	path := "identity-provider/instances/" + alias

	current, present, err := kcadmShow(ctx, conn, path, realm)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok("Identity provider " + alias + " does not exist"), nil
		}
		res, err := kcadmDelete(ctx, conn, path, realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_identity_provider", "unable to delete identity provider "+alias, res), nil
		}
		return Changed("Identity provider " + alias + " has been deleted"), nil
	}

	var config map[string]string
	if raw, ok := args["config"].(map[string]any); ok {
		config = keycloakStringifyConfigMap(raw)
	}

	if !present {
		body := map[string]any{"alias": alias}
		if pid := argString(args, "provider_id", ""); pid != "" {
			body["providerId"] = pid
		}
		for _, f := range keycloakIDPFields {
			if v, ok := args[f.arg]; ok {
				body[f.api] = keycloakFieldValue(f.kind, v)
			}
		}
		if config != nil {
			body["config"] = config
		}
		res, err := kcadmCreateBody(ctx, conn, "identity-provider/instances", realm, body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_identity_provider", "unable to create identity provider "+alias, res), nil
		}
		if err := keycloakSyncIDPMappers(ctx, conn, realm, alias, args); err != nil {
			return Result{}, err
		}
		return Changed("Identity provider " + alias + " has been created"), nil
	}

	changed := false
	body := map[string]any{}
	for k, v := range current {
		body[k] = v
	}
	for _, f := range keycloakIDPFields {
		if v, ok := args[f.arg]; ok {
			want := keycloakFieldValue(f.kind, v)
			if have, existed := current[f.api]; !existed || !jsonScalarEqual(want, have) {
				body[f.api] = want
				changed = true
			}
		}
	}
	if config != nil {
		haveConfig, _ := current["config"].(map[string]any)
		mergedConfig := map[string]any{}
		for k, v := range haveConfig {
			mergedConfig[k] = v
		}
		for k, v := range config {
			if hv, existed := haveConfig[k]; !existed || fmt.Sprint(hv) != v || k == "clientSecret" {
				changed = true
			}
			mergedConfig[k] = v
		}
		body["config"] = mergedConfig
	}

	if changed {
		res, err := kcadmUpdateBody(ctx, conn, path, realm, body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_identity_provider", "unable to update identity provider "+alias, res), nil
		}
	}
	mappersChanged, err := keycloakSyncIDPMappersChanged(ctx, conn, realm, alias, args)
	if err != nil {
		return Result{}, err
	}
	if changed || mappersChanged {
		return Changed("Identity provider " + alias + " has been updated"), nil
	}
	return Ok("Identity provider " + alias + " already up to date"), nil
}

// keycloakFieldValue coerces v per kind ("bool"|"string"|otherwise
// passthrough).
func keycloakFieldValue(kind string, v any) any {
	switch kind {
	case "bool":
		if b, ok := v.(bool); ok {
			return b
		}
		return fmt.Sprint(v) == "true"
	default:
		return fmt.Sprint(v)
	}
}

// keycloakStringifyConfigMap coerces every value of a config dict to
// its string form — every IdentityProviderRepresentation.config (and
// UserFederation ComponentRepresentation.config) entry is a plain
// string in the real Keycloak API, regardless of the YAML type a task
// gives it.
func keycloakStringifyConfigMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch b := v.(type) {
		case bool:
			out[k] = fmt.Sprintf("%t", b)
		default:
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

// keycloakSyncIDPMappers creates every entry of args["mappers"] fresh —
// used on create, where there is no existing mapper set to diff
// against.
func keycloakSyncIDPMappers(ctx context.Context, conn remoteexec.Connection, realm, alias string, args map[string]any) error {
	list, _ := args["mappers"].([]any)
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		body := map[string]any{
			"name":                   argString(m, "name", ""),
			"identityProviderAlias":  alias,
			"identityProviderMapper": argString(m, "identityProviderMapper", ""),
		}
		if cfg, ok := m["config"].(map[string]any); ok {
			body["config"] = keycloakStringifyConfigMap(cfg)
		}
		res, err := kcadmCreateBody(ctx, conn, "identity-provider/instances/"+alias+"/mappers", realm, body)
		if err != nil {
			return err
		}
		if res.RC != 0 {
			return errArg("keycloak_identity_provider: unable to create mapper %q: %s", argString(m, "name", ""), res.Stderr)
		}
	}
	return nil
}

// keycloakSyncIDPMappersChanged diffs args["mappers"] (if given) against
// the identity provider's own current mapper set (matched by name),
// creating missing ones, updating changed ones, and — since `mappers`
// was explicitly given — deleting any current mapper not named in the
// list (see moduleKeycloakIdentityProvider's own doc comment).
func keycloakSyncIDPMappersChanged(ctx context.Context, conn remoteexec.Connection, realm, alias string, args map[string]any) (bool, error) {
	list, ok := args["mappers"].([]any)
	if !ok {
		return false, nil
	}
	var current []map[string]any
	res, err := kcadmGetJSON(ctx, conn, "identity-provider/instances/"+alias+"/mappers", realm, nil, &current)
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
		body := map[string]any{
			"name":                   name,
			"identityProviderAlias":  alias,
			"identityProviderMapper": argString(m, "identityProviderMapper", ""),
		}
		if cfg, ok := m["config"].(map[string]any); ok {
			body["config"] = keycloakStringifyConfigMap(cfg)
		}
		if existing, found := currentByName[name]; found {
			id, _ := existing["id"].(string)
			if !jsonScalarEqual(body["identityProviderMapper"], existing["identityProviderMapper"]) {
				changed = true
			}
			body["id"] = id
			res, err := kcadmUpdateBody(ctx, conn, "identity-provider/instances/"+alias+"/mappers/"+id, realm, body)
			if err != nil {
				return false, err
			}
			if res.RC != 0 {
				return false, errArg("keycloak_identity_provider: unable to update mapper %q: %s", name, res.Stderr)
			}
			continue
		}
		res, err := kcadmCreateBody(ctx, conn, "identity-provider/instances/"+alias+"/mappers", realm, body)
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, errArg("keycloak_identity_provider: unable to create mapper %q: %s", name, res.Stderr)
		}
		changed = true
	}
	for name, c := range currentByName {
		if desiredNames[name] {
			continue
		}
		id, _ := c["id"].(string)
		res, err := kcadmDelete(ctx, conn, "identity-provider/instances/"+alias+"/mappers/"+id, realm)
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, errArg("keycloak_identity_provider: unable to delete mapper %q: %s", name, res.Stderr)
		}
		changed = true
	}
	return changed, nil
}
