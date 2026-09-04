package modules

import (
	"context"
	"sort"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakRealmLocalization implements Ansible's
// `keycloak_realm_localization` (community.general) module: manages
// per-locale message text overrides for a realm — see
// keycloak_common.go's own doc comment for the kcadm.sh substitution.
//
// The whole-locale set is read via `kcadm.sh get
// localization/<locale> -r <realm>` (module_utils' own
// URL_LOCALIZATIONS, `GET .../localization/{locale}`, returning a flat
// JSON object `{key: value, ...}`) — a clean, ordinary JSON GET this
// port replicates exactly. Real keycloak_realm_localization's own
// per-key WRITE, however (module_utils' own set_localization_value,
// `PUT .../localization/{locale}/{key}` with a `Content-Type: text/plain`
// body containing the raw value string, NOT a JSON document) has NO
// faithful equivalent through kcadm.sh: kcadm's `update`/`create`
// verbs always send `Content-Type: application/json`, with no flag to
// override that. This port still issues one PUT per key that needs
// creating or changing (`kcadm.sh update localization/<locale>/<key>
// -r <realm> -f -`, piping the JSON-STRING encoding of the value, e.g.
// `"Hello"` rather than the bare `Hello` text/plain body), matching the
// real call shape as closely as kcadm allows — but this is an
// HONESTLY-FLAGGED, UNVERIFIED best-effort substitution: if Keycloak's
// own JAX-RS resource for this endpoint strictly enforces
// `@Consumes(MediaType.TEXT_PLAIN)` (plausible, and this port had no
// live server to check), the real server will reject a JSON body with
// 415 Unsupported Media Type and this one write path will fail — the
// single biggest unverified deviation in this whole batch, called out
// here rather than silently assumed to work. Per-key DELETE (`kcadm.sh
// delete localization/<locale>/<key> -r <realm>`, no request body at
// all) carries none of this risk and is fully faithful.
//
// Args: parent_id (required — the realm name); locale (required);
// overrides (list of {key, value} — value defaults to ""); force
// (default false); state (present|absent, default present) — see real
// keycloak_realm_localization's own doc for the exact force × state
// interaction (force=true on state=present removes any current key not
// listed in overrides; force=true on state=absent removes every key for
// the locale regardless of overrides; force=false only ever touches the
// keys explicitly listed).
func moduleKeycloakRealmLocalization(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_realm_localization"); !ok {
		return res, nil
	}
	realm, err := requireString(args, "parent_id")
	if err != nil {
		return Result{}, err
	}
	locale, err := requireString(args, "locale")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_realm_localization: state must be one of present, absent, got %q", state)
	}
	force := argBool(args, "force", false)
	path := "localization/" + locale

	var current map[string]string
	res, err := kcadmGetJSON(ctx, conn, path, realm, nil, &current)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		current = map[string]string{}
	}
	if current == nil {
		current = map[string]string{}
	}

	overridesRaw, _ := args["overrides"].([]any)
	desired := map[string]string{}
	var desiredOrder []string
	for _, o := range overridesRaw {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		k := argString(m, "key", "")
		if k == "" {
			continue
		}
		desired[k] = argString(m, "value", "")
		desiredOrder = append(desiredOrder, k)
	}

	changed := false

	if state == "absent" {
		if force {
			if len(current) > 0 {
				dres, err := kcadmDelete(ctx, conn, path, realm)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return kcadmFailedf("keycloak_realm_localization", "unable to delete all overrides for locale "+locale, dres), nil
				}
				changed = true
			}
			return keycloakLocalizationResult(changed, locale, nil), nil
		}
		for _, k := range desiredOrder {
			if _, ok := current[k]; !ok {
				continue
			}
			dres, err := kcadmDelete(ctx, conn, path+"/"+k, realm)
			if err != nil {
				return Result{}, err
			}
			if dres.RC != 0 {
				return kcadmFailedf("keycloak_realm_localization", "unable to delete override "+k+" for locale "+locale, dres), nil
			}
			delete(current, k)
			changed = true
		}
		return keycloakLocalizationResult(changed, locale, current), nil
	}

	if force {
		for k := range current {
			if _, ok := desired[k]; ok {
				continue
			}
			dres, err := kcadmDelete(ctx, conn, path+"/"+k, realm)
			if err != nil {
				return Result{}, err
			}
			if dres.RC != 0 {
				return kcadmFailedf("keycloak_realm_localization", "unable to remove stale override "+k+" for locale "+locale, dres), nil
			}
			delete(current, k)
			changed = true
		}
	}
	for _, k := range desiredOrder {
		v := desired[k]
		if have, ok := current[k]; ok && have == v {
			continue
		}
		ures, err := kcadmUpdateBody(ctx, conn, path+"/"+k, realm, v)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return kcadmFailedf("keycloak_realm_localization", "unable to set override "+k+" for locale "+locale, ures), nil
		}
		current[k] = v
		changed = true
	}

	return keycloakLocalizationResult(changed, locale, current), nil
}

func keycloakLocalizationResult(changed bool, locale string, finalMap map[string]string) Result {
	var overrides []map[string]any
	keys := make([]string, 0, len(finalMap))
	for k := range finalMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		overrides = append(overrides, map[string]any{"key": k, "value": finalMap[k]})
	}
	if overrides == nil {
		overrides = []map[string]any{}
	}
	endState := map[string]any{"locale": locale, "overrides": overrides}
	var r Result
	if changed {
		r = Changed("Localization overrides for locale " + locale + " have been updated")
	} else {
		r = Ok("Localization overrides for locale " + locale + " already up to date")
	}
	return r.WithExtra("end_state", endState)
}
