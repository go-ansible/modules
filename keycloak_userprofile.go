package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// keycloakUserProfileProviderID/Type are the Keycloak component
// providerId/providerType for a realm's declarative user profile
// configuration — matching this module's own doc defaults exactly
// (both are effectively fixed values: `provider_id`'s only real choice
// is `declarative-user-profile`, `provider_type`'s only real choice is
// `org.keycloak.userprofile.UserProfileProvider`).
const (
	keycloakUserProfileProviderID   = "declarative-user-profile"
	keycloakUserProfileProviderType = "org.keycloak.userprofile.UserProfileProvider"
	// keycloakUserProfileConfigKey is the single component config key
	// Keycloak itself stores the whole declarative User Profile schema
	// under — verified against Keycloak's own UserProfileProvider
	// component model (`kc.user.profile.config`, a component config
	// MultivaluedHashMap entry whose sole value is the JSON-encoded
	// UPConfig document, not a set of individual `-s config.*=` fields).
	keycloakUserProfileConfigKey = "kc.user.profile.config"
)

// moduleKeycloakUserprofile implements Ansible's `keycloak_userprofile`
// (community.general) module: creates, updates, or deletes a realm's
// declarative User Profile configuration, via `kcadm.sh
// create/get/update/delete components(/<id>)` with
// `providerId=declarative-user-profile`/
// `providerType=org.keycloak.userprofile.UserProfileProvider` — a user
// profile is a Keycloak "component" under the hood, the same resource
// keycloak_realm_key.go and keycloak_user_federation.go also manage
// under different providerTypes — see keycloak_common.go's own doc
// comment for the kcadm.sh substitution.
//
// Args: parent_id (required — the realm name, also usable via its
// `parentId`/`realm` aliases); config.kc_user_profile_config (a list
// whose FIRST element is the actual desired UPConfig document —
// attributes/groups/unmanagedAttributePolicy — matching this module's
// own slightly unusual "single-element list" shape exactly); state
// (present|absent, default present).
//
// The whole UPConfig document is JSON-encoded and sent as
// `config["kc.user.profile.config"]=["<json>"]` — a single component
// config entry, NOT a set of individual `-s config.*=value` tokens,
// because the key `kc.user.profile.config` itself contains literal
// dots, which would collide with kcadm's own `-s parent.child=value`
// nested-path syntax (see keycloak_common.go's own doc comment on when
// this port uses a raw `-f -` body instead of `-s` for exactly this
// reason). Every map key anywhere inside the UPConfig document is
// passed through keycloakSnakeToCamel first — this module's own doc
// says it "also accepts the camelCase versions of the options" for
// compatibility, and since a task may write EITHER spelling at any
// nesting depth (`display_name`/`displayName`,
// `unmanaged_attribute_policy`/`unmanagedAttributePolicy`,
// `display_header`/`displayHeader`, and so on through
// attributes[].validations.*), this port normalizes every key
// generically (any `snake_case` key becomes `camelCase`; an
// already-camelCase key is left unchanged) rather than hand-listing
// every individual suboption name, which would be one more mechanical
// but error-prone table for a deeply nested, evolving schema.
func moduleKeycloakUserprofile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_userprofile"); !ok {
		return res, nil
	}
	realm := argStringAliased(args, "parent_id", "parentId", argString(args, "realm", ""))
	if realm == "" {
		return Result{}, errArg("keycloak_userprofile: parent_id is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_userprofile: state must be one of present, absent, got %q", state)
	}

	var matches []map[string]any
	res, err := kcadmGetJSON(ctx, conn, "components", realm, []string{
		"parent=" + realm, "type=" + keycloakUserProfileProviderType,
	}, &matches)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_userprofile", "unable to look up user profile provider", res), nil
	}
	var current map[string]any
	present := len(matches) > 0
	if present {
		current = matches[0]
	}

	if state == "absent" {
		if !present {
			return Ok("User Profile provider does not exist"), nil
		}
		id, _ := current["id"].(string)
		dres, err := kcadmDelete(ctx, conn, "components/"+id, realm)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return kcadmFailedf("keycloak_userprofile", "unable to delete User Profile provider", dres), nil
		}
		return Changed("User Profile provider has been deleted"), nil
	}

	var upConfigJSON string
	if cfg, ok := args["config"].(map[string]any); ok {
		if list, ok := cfg["kc_user_profile_config"].([]any); ok && len(list) > 0 {
			normalized := keycloakNormalizeUPConfigKeys(list[0])
			b, err := json.Marshal(normalized)
			if err != nil {
				return Result{}, err
			}
			upConfigJSON = string(b)
		}
	}

	if !present {
		componentCfg := map[string]any{}
		if upConfigJSON != "" {
			componentCfg[keycloakUserProfileConfigKey] = []string{upConfigJSON}
		}
		body := map[string]any{
			"name":         keycloakUserProfileProviderID,
			"providerId":   keycloakUserProfileProviderID,
			"providerType": keycloakUserProfileProviderType,
			"parentId":     realm,
			"config":       componentCfg,
		}
		res, err := kcadmCreateBody(ctx, conn, "components", realm, body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_userprofile", "unable to create User Profile provider", res), nil
		}
		return Changed("UserProfileProvider created successfully"), nil
	}

	id, _ := current["id"].(string)
	haveConfig, _ := current["config"].(map[string]any)
	haveJSON := ""
	if arr, ok := haveConfig[keycloakUserProfileConfigKey].([]any); ok && len(arr) > 0 {
		haveJSON = argString(map[string]any{"v": arr[0]}, "v", "")
	}
	if upConfigJSON == "" || upConfigJSON == haveJSON {
		return Ok("UserProfileProvider already up to date"), nil
	}
	body := map[string]any{}
	for k, v := range current {
		body[k] = v
	}
	newConfig := map[string]any{}
	for k, v := range haveConfig {
		newConfig[k] = v
	}
	newConfig[keycloakUserProfileConfigKey] = []string{upConfigJSON}
	body["config"] = newConfig
	res, err = kcadmUpdateBody(ctx, conn, "components/"+id, realm, body)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_userprofile", "unable to update User Profile provider", res), nil
	}
	return Changed("UserProfileProvider updated successfully"), nil
}

// keycloakSnakeToCamel converts one_snake_case_key to oneSnakeCaseKey;
// a key with no underscore (already camelCase) passes through
// unchanged.
func keycloakSnakeToCamel(s string) string {
	if !strings.Contains(s, "_") {
		return s
	}
	parts := strings.Split(s, "_")
	out := parts[0]
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

// keycloakNormalizeUPConfigKeys recursively applies keycloakSnakeToCamel
// to every map key within v (maps and slices are walked; scalars are
// returned unchanged) — see moduleKeycloakUserprofile's own doc
// comment.
func keycloakNormalizeUPConfigKeys(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[keycloakSnakeToCamel(k)] = keycloakNormalizeUPConfigKeys(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = keycloakNormalizeUPConfigKeys(e)
		}
		return out
	default:
		return v
	}
}
