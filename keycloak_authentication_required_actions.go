package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakAuthenticationRequiredActions implements Ansible's
// `keycloak_authentication_required_actions` (community.general)
// module: registers, updates, or deletes a realm's required actions
// (e.g. TERMS_AND_CONDITIONS, UPDATE_PASSWORD, CONFIGURE_TOTP), via
// kcadm.sh's own `authentication/register-required-action` (create),
// `authentication/required-actions` (list), and
// `authentication/required-actions/<alias>` (get/update/delete)
// resource paths — verified against
// module_utils/_keycloak.py's own URL_AUTHENTICATION_REGISTER_
// REQUIRED_ACTION/URL_AUTHENTICATION_REQUIRED_ACTIONS/
// URL_AUTHENTICATION_REQUIRED_ACTIONS_ALIAS constants.
//
// Args: realm (required); required_actions (list of dicts: alias
// (required) + name and providerId (both required only when
// REGISTERING a new required action — Keycloak's own
// register-required-action endpoint needs both) + optional enabled
// (bool), defaultAction (bool), priority (int), config (dict));
// state (present|absent, required — no default).
//
// Per the real module's own doc, duplicate required_actions entries
// (by alias) are filtered, the first occurrence winning — this port
// does the same.
//
// Idempotency: each required_actions[] entry is looked up by alias
// against the realm's current required-actions list. state=absent
// deletes a match (Changed=false if already absent). state=present
// REGISTERS a not-yet-existing alias (via register-required-action,
// which only accepts providerId+name — both required in that case,
// matching real keycloak_authentication_required_actions.py's own
// argument validation) then, in the same run, updates it with every
// other given field (enabled/defaultAction/priority/config); an
// ALREADY-existing alias is merged (kcMergeChangeset) with whichever
// fields were given and PUT back only if something actually differs.
func moduleKeycloakAuthenticationRequiredActions(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_authentication_required_actions"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm, err := requireString(args, "realm")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	requested := argListOfMaps(args, "required_actions")

	// Filter out duplicate aliases, first occurrence wins.
	seen := map[string]bool{}
	var actions []map[string]any
	for _, ra := range requested {
		alias := kcString(ra, "alias")
		if alias == "" || seen[alias] {
			continue
		}
		seen[alias] = true
		actions = append(actions, ra)
	}

	current, res, err := kcadmListMaps(ctx, conn, "authentication/required-actions", realm, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf(mod, "list required actions in realm "+realm, res), nil
	}

	changed := false
	var endStates []map[string]any
	var msgs []string
	for _, ra := range actions {
		alias := kcString(ra, "alias")
		existing := kcFindByField(current, "alias", alias)

		if state == "absent" {
			if existing == nil {
				msgs = append(msgs, fmt.Sprintf("required action %s already absent", alias))
				continue
			}
			dres, err := kcadmDelete(ctx, conn, "authentication/required-actions/"+alias, realm)
			if err != nil {
				return Result{}, err
			}
			if dres.RC != 0 {
				return kcadmFailedf(mod, "delete required action "+alias, dres), nil
			}
			changed = true
			msgs = append(msgs, fmt.Sprintf("required action %s deleted", alias))
			continue
		}

		changeset := map[string]any{}
		kcSetIfPresent(changeset, ra, "enabled", "enabled")
		kcSetIfPresent(changeset, ra, "defaultAction", "defaultAction")
		kcSetIfPresent(changeset, ra, "priority", "priority")
		kcSetIfPresent(changeset, ra, "config", "config")

		if existing == nil {
			name := kcString(ra, "name")
			providerID := kcString(ra, "providerId")
			if name == "" || providerID == "" {
				return Result{}, errArg("%s: required_actions[alias=%s]: name and providerId are both required "+
					"when registering a new required action", mod, alias)
			}
			cres, err := kcadmCreateBody(ctx, conn, "authentication/register-required-action", realm,
				map[string]any{"providerId": providerID, "name": name})
			if err != nil {
				return Result{}, err
			}
			if cres.RC != 0 {
				return kcadmFailedf(mod, "register required action "+alias, cres), nil
			}
			changed = true
			if len(changeset) > 0 {
				var reg map[string]any
				gres, err := kcadmGetJSON(ctx, conn, "authentication/required-actions/"+alias, realm, nil, &reg)
				if err != nil {
					return Result{}, err
				}
				if gres.RC == 0 {
					merged, _ := kcMergeChangeset(reg, changeset)
					ures, err := kcadmUpdateBody(ctx, conn, "authentication/required-actions/"+alias, realm, merged)
					if err != nil {
						return Result{}, err
					}
					if ures.RC != 0 {
						return kcadmFailedf(mod, "update newly registered required action "+alias, ures), nil
					}
				}
			}
			msgs = append(msgs, fmt.Sprintf("required action %s registered", alias))
			var final map[string]any
			if res, err := kcadmGetJSON(ctx, conn, "authentication/required-actions/"+alias, realm, nil, &final); err != nil {
				return Result{}, err
			} else if res.RC == 0 {
				endStates = append(endStates, final)
			}
			continue
		}

		merged, itemChanged := kcMergeChangeset(existing, changeset)
		if !itemChanged {
			msgs = append(msgs, fmt.Sprintf("required action %s already up to date", alias))
			endStates = append(endStates, existing)
			continue
		}
		ures, err := kcadmUpdateBody(ctx, conn, "authentication/required-actions/"+alias, realm, merged)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return kcadmFailedf(mod, "update required action "+alias, ures), nil
		}
		changed = true
		msgs = append(msgs, fmt.Sprintf("required action %s updated", alias))
		endStates = append(endStates, merged)
	}

	msg := ""
	for i, m := range msgs {
		if i > 0 {
			msg += "; "
		}
		msg += m
	}
	r := Result{Changed: changed, Msg: msg}
	return r.WithExtra("end_state", endStates), nil
}
