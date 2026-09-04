package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// keycloakRealmKeyProviderType is the Keycloak component providerType
// for every realm key — verified against Keycloak's own component
// model (org.keycloak.keys.KeyProvider is the SPI every key provider,
// generated or imported, registers under), shared with
// keycloak_component_info.go's own provider_type examples.
const keycloakRealmKeyProviderType = "org.keycloak.keys.KeyProvider"

// keycloakRealmKeyConfigFields maps keycloak_realm_key's own
// `config.*` suboptions to their Keycloak component `config.*` key.
// priority/active/enabled/algorithm/certificate/private_key/key_size/
// secret_size/keystore/keystore_password/key_alias/key_password are
// verified against Keycloak's own key-provider component schema
// (RsaKeyProviderFactory/HmacKeyProviderFactory/
// AesGeneratedKeyProviderFactory/JavaKeystoreKeyProviderFactory all use
// these exact property names). `elliptic_curve` is NOT verified to the
// same confidence: real Keycloak spells this differently per provider
// (`ecdsaEllipticCurveKey` for ecdsa-generated, a differently-named
// property again for ecdh-generated/eddsa-generated) and this port had
// no live server to confirm the exact spelling for each — it sends
// `config.ellipticCurve` uniformly instead, an honestly-flagged
// best-effort mapping rather than a verified one.
var keycloakRealmKeyConfigFields = []struct {
	arg, api, kind string
}{
	{"priority", "priority", "int"},
	{"active", "active", "bool"},
	{"enabled", "enabled", "bool"},
	{"algorithm", "algorithm", "string"},
	{"certificate", "certificate", "string"},
	{"private_key", "privateKey", "string"},
	{"key_size", "keySize", "int"},
	{"secret_size", "secretSize", "int"},
	{"keystore", "keystore", "string"},
	{"keystore_password", "keystorePassword", "string"},
	{"key_alias", "keyAlias", "string"},
	{"key_password", "keyPassword", "string"},
	{"elliptic_curve", "ellipticCurve", "string"},
}

// moduleKeycloakRealmKey implements Ansible's `keycloak_realm_key`
// (community.general) module: creates, updates, or deletes a realm's
// key provider component, via `kcadm.sh create/get/update/delete
// components(/<id>)` with `providerType=org.keycloak.keys.KeyProvider`
// — realm keys are Keycloak "components" under the hood, the same
// resource keycloak_component_info.go reads and
// keycloak_userprofile.go also manages under a different providerType
// — see keycloak_common.go's own doc comment for the kcadm.sh
// substitution.
//
// Args: name (required); parent_id (required — the realm name);
// provider_id (default rsa); config (dict — see
// keycloakRealmKeyConfigFields above; every value is sent as a
// single-element JSON string array, matching the Keycloak API's own
// MultivaluedHashMap<String,String> shape for component config, e.g.
// `config.priority=["120"]`, never a bare `120`); state
// (present|absent, default present); force — accepted, but see the
// deviation below; update_password (always|on_create, default always) —
// gates whether `config.keystore_password`/`config.key_password` are
// included on an UPDATE of an existing component (on_create sends them
// only when creating; always sends them whenever given, matching real
// keycloak_realm_key's own documented idempotency trade-off for
// java-keystore passwords, which Keycloak always masks on read).
//
// Lookup: an existing key component is found via `kcadm.sh get
// components -r <realm> -q parent=<parent_id> -q
// type=org.keycloak.keys.KeyProvider -q name=<name>`, matching real
// keycloak_realm_key's own `get_components` filter exactly.
//
// Deviation from real keycloak_realm_key: `config.private_key` and
// `config.certificate` can never be compared against Keycloak's own
// current value (Keycloak does not return the private key at all, and
// an auto-generated certificate has no stable "desired" baseline
// either — matching this module's own documented NOTES section
// exactly). This port therefore always includes both in the update
// body whenever the task provides them, unconditionally — the same
// outcome real keycloak_realm_key's own `force=true` produces, made
// the unconditional behavior here rather than gated further behind
// `force` (which this port accepts for argument-shape compatibility
// but which changes nothing beyond what already happens), since there
// is no meaningfully MORE conservative default this port could offer
// without simply never updating those two fields at all.
func moduleKeycloakRealmKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_realm_key"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	parentID, err := requireString(args, "parent_id")
	if err != nil {
		return Result{}, err
	}
	realm := parentID
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_realm_key: state must be one of present, absent, got %q", state)
	}
	providerID := argString(args, "provider_id", "rsa")

	var matches []map[string]any
	res, err := kcadmGetJSON(ctx, conn, "components", realm, []string{
		"parent=" + parentID, "type=" + keycloakRealmKeyProviderType, "name=" + name,
	}, &matches)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_realm_key", "unable to look up realm key "+name, res), nil
	}
	var current map[string]any
	present := false
	for _, m := range matches {
		if s, _ := m["name"].(string); s == name {
			current = m
			present = true
			break
		}
	}

	if state == "absent" {
		if !present {
			return Ok("Realm key " + name + " does not exist"), nil
		}
		id, _ := current["id"].(string)
		res, err := kcadmDelete(ctx, conn, "components/"+id, realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_realm_key", "unable to delete realm key "+name, res), nil
		}
		return Changed("Realm key " + name + " has been deleted"), nil
	}

	config, ok := args["config"].(map[string]any)
	if !ok {
		config = map[string]any{}
	}
	updatePasswordMode := argString(args, "update_password", "always")

	if !present {
		cfg := map[string]any{}
		for _, f := range keycloakRealmKeyConfigFields {
			if v, ok := config[f.arg]; ok {
				cfg[f.api] = []string{keycloakConfigValueString(f.kind, v)}
			}
		}
		body := map[string]any{
			"name":         name,
			"providerId":   providerID,
			"providerType": keycloakRealmKeyProviderType,
			"parentId":     parentID,
			"config":       cfg,
		}
		id, res, err := kcadmCreateBodyID(ctx, conn, "components", realm, body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_realm_key", "unable to create realm key "+name, res), nil
		}
		r := Changed("Realm key " + name + " has been created")
		return r.WithExtra("end_state", map[string]any{"id": id, "name": name, "providerId": providerID, "parentId": parentID}), nil
	}

	id, _ := current["id"].(string)
	haveConfig, _ := current["config"].(map[string]any)
	body := map[string]any{}
	for k, v := range current {
		body[k] = v
	}
	changed := false
	newConfig := map[string]any{}
	for k, v := range haveConfig {
		newConfig[k] = v
	}
	for _, f := range keycloakRealmKeyConfigFields {
		v, ok := config[f.arg]
		if !ok {
			continue
		}
		if (f.arg == "keystore_password" || f.arg == "key_password") && updatePasswordMode == "on_create" {
			continue
		}
		want := keycloakConfigValueString(f.kind, v)
		if f.arg == "private_key" || f.arg == "certificate" {
			// Never comparable against the current state — see
			// moduleKeycloakRealmKey's own doc comment.
			newConfig[f.api] = []string{want}
			changed = true
			continue
		}
		have := ""
		if arr, ok := haveConfig[f.api].([]any); ok && len(arr) > 0 {
			have = argString(map[string]any{"v": arr[0]}, "v", "")
		}
		if have != want {
			newConfig[f.api] = []string{want}
			changed = true
		}
	}
	body["config"] = newConfig

	if !changed {
		return Ok("Realm key " + name + " already up to date"), nil
	}
	res, err = kcadmUpdateBody(ctx, conn, "components/"+id, realm, body)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_realm_key", "unable to update realm key "+name, res), nil
	}
	return Changed("Realm key " + name + " has been updated"), nil
}

// keycloakConfigValueString coerces one config suboption value to the
// plain string a component's MultivaluedHashMap<String,String> config
// entry needs (this port always wraps it as a single-element JSON
// array itself — see keycloakRealmKeyConfigFields's own doc comment).
func keycloakConfigValueString(kind string, v any) string {
	switch kind {
	case "bool":
		return strconv.FormatBool(argBool(map[string]any{"v": v}, "v", false))
	case "int":
		return strconv.Itoa(argInt(map[string]any{"v": v}, "v", 0))
	default:
		return argString(map[string]any{"v": v}, "v", "")
	}
}
