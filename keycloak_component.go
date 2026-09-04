package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakComponent implements Ansible's `keycloak_component`
// (community.general) module: manages a realm component (a user
// storage provider, a key provider, ...), via kcadm.sh's own
// `components` (list/create) and `components/<id>` (update/delete)
// resource paths, filtering the list by `-q type=<provider_type>`
// (matching real KeycloakAPI.get_components(urlencode(dict(type=
// provider_type)), parent_id)'s own server-side query — parent_id is
// passed there as the METHOD'S OWN realm argument, not a query filter:
// the REST path itself is already realm-scoped, so parent_id doubles
// as this port's own kcadm.sh `-r` realm too) — verified against
// module_utils/_keycloak.py's own URL_COMPONENT(S) and
// get_components's own positional-argument mapping.
//
// Args: parent_id (required — in practice the realm's own name, or
// another component's own id for a nested component); name (required);
// provider_id (required); provider_type (required — a fully-qualified
// Keycloak SPI name, e.g. "org.keycloak.storage.UserStorageProvider");
// config (dict); state (present|absent, default present).
//
// A component's own `config` map is Keycloak's own
// MultivaluedHashMap<String, String> shape: every value, even a
// conceptually scalar one, is sent as a JSON array of ONE string
// (booleans lowercased) — matching real keycloak_component.py's own
// documented loop exactly (`changeset["config"][camel(config_param)] =
// [str(raw_value).lower() if bool else str(raw_value)]`) — see
// kcComponentConfigValue.
//
// Idempotency: components are listed (filtered server-side by
// provider_type and parent_id) and matched by name — matching real
// keycloak_component.py's own loop exactly (it does NOT look up by a
// component id at all, only by name within that filtered list).
// kcMergeChangeset (name/providerId/providerType/parentId/config)
// decides whether an update is needed.
func moduleKeycloakComponent(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_component"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	parentID, err := requireString(args, "parent_id")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	providerID, err := requireString(args, "provider_id")
	if err != nil {
		return Result{}, err
	}
	providerType, err := requireString(args, "provider_type")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}

	// Keycloak components live directly under "admin/realms/{realm}/components",
	// with parent_id (the realm's own name, typically) as a query filter,
	// not a -r realm scope — parent_id addresses which realm/parent, and
	// kcadm's own "components" path is queried with no realm at all
	// (matching real KeycloakAPI.get_components, which builds its own
	// query string with both type and parent, never using the "realm"
	// path segment for anything but the connection's own baseurl).
	current, res, err := kcadmListMaps(ctx, conn, "components", parentID, []string{"type=" + providerType})
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf(mod, "list components of type "+providerType+" under "+parentID, res), nil
	}
	existing := kcFindByField(current, "name", name)

	changeset := map[string]any{
		"name":         name,
		"providerId":   providerID,
		"providerType": providerType,
		"parentId":     parentID,
		"config":       kcComponentConfig(argMapAny(args, "config")),
	}

	if existing == nil {
		if state == "absent" {
			return Ok(fmt.Sprintf("Component %s does not exist; doing nothing.", name)), nil
		}
		cres, err := kcadmCreateBody(ctx, conn, "components", parentID, changeset)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return kcadmFailedf(mod, "create component "+name, cres), nil
		}
		current2, _, err := kcadmListMaps(ctx, conn, "components", parentID, []string{"type=" + providerType})
		if err != nil {
			return Result{}, err
		}
		after := kcFindByField(current2, "name", name)
		return Changed(fmt.Sprintf("Component %s created", name)).WithExtra("end_state", after), nil
	}

	if state == "absent" {
		dres, err := kcadmDelete(ctx, conn, "components/"+kcString(existing, "id"), parentID)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return kcadmFailedf(mod, "delete component "+name, dres), nil
		}
		return Changed(fmt.Sprintf("Component %s deleted", name)), nil
	}

	merged, changed := kcMergeChangeset(existing, changeset)
	if !changed {
		return Ok(fmt.Sprintf("Component %s was in sync", name)).WithExtra("end_state", existing), nil
	}
	merged["id"] = kcString(existing, "id")
	ures, err := kcadmUpdateBody(ctx, conn, "components/"+kcString(existing, "id"), parentID, merged)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return kcadmFailedf(mod, "update component "+name, ures), nil
	}
	return Changed(fmt.Sprintf("Component %s changed", name)).WithExtra("end_state", merged), nil
}

// kcComponentConfig converts a plain config dict into Keycloak's own
// MultivaluedHashMap shape: each value becomes a one-element []string
// (a bool is lowercased, matching real keycloak_component.py's own
// str(raw_value).lower() for bool values).
func kcComponentConfig(config map[string]any) map[string][]string {
	out := map[string][]string{}
	for k, v := range config {
		if b, ok := v.(bool); ok {
			if b {
				out[k] = []string{"true"}
			} else {
				out[k] = []string{"false"}
			}
			continue
		}
		out[k] = []string{fmt.Sprint(v)}
	}
	return out
}
