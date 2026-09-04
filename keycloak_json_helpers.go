package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file adds a few small, generic helpers shared by this batch's
// own 17 keycloak_* modules (keycloak_authentication*, keycloak_authz_*,
// keycloak_client*, keycloak_clientscope*, keycloak_clientsecret_*,
// keycloak_clienttemplate, keycloak_component) on top of
// keycloak_common.go's own kcadm.sh plumbing (written by a sibling
// agent working sixteen OTHER keycloak_* modules in this same batch —
// see that file's own doc comment for the kcadm.sh syntax/auth-
// precondition decisions every keycloak_* module in this batch shares;
// this file does not repeat them). It is a separate file, not an
// addition to keycloak_common.go itself, specifically to avoid a
// concurrent-write collision with that sibling agent's own in-progress
// edits to that file.
//
// Every module in THIS batch builds its Keycloak Admin REST API request
// bodies as a plain Go map[string]any and passes it straight to
// keycloak_common.go's own kcadmCreateBody/kcadmCreateBodyID/
// kcadmUpdateBody (which json.Marshal it and pipe it to kcadm.sh's own
// `-f -`), rather than building a `-s field=value` token sequence via
// kcadmSet/kcadmSetJSON — simpler and less error-prone for the
// deeply-nested bodies several of these modules' own real arguments
// require (protocol_mappers, attributes, authenticationExecutions,
// authorization_settings, ...), and exactly the kind of case
// keycloak_common.go's own doc comment already calls out
// kcadmCreateBody/kcadmUpdateBody as being for.

// kcadmDeleteBody runs `kcadm.sh delete <path> [-r realm] -f -`, piping
// body's JSON encoding over stdin — needed for the handful of
// Keycloak scope-mappings endpoints (client_rolescope.go/
// clientscope_rolemappings.go's own `scope-mappings/...` paths) whose
// DELETE method itself takes a JSON array body (the specific role
// representations to unmap), unlike every other resource this batch
// deletes by bare path alone. kcadm.sh has no dedicated convenience
// command for scope-mappings the way it does for user/group role
// mappings (keycloak_common.go's own add-roles/remove-roles — those
// apply only to a --uid/--gid target, not a client's or client-scope's
// own scope-mappings), so this port relies on kcadm.sh's generic
// `delete` verb accepting `-f -` the same way `create`/`update`
// document it does — inferred from kcadm.sh's own consistent
// four-verb design (the same create/get/update/delete parser
// handling all four, per keycloak_common.go's own doc comment), not
// independently confirmed against a live kcadm.sh binary in this
// sandbox (no kcadm.sh was available to exercise directly — see
// keycloak_common.go's own doc comment on that same constraint for
// glab): if a future kcadm.sh version's `delete` rejects `-f`, this is
// the one call site in this batch that assumption would break.
func kcadmDeleteBody(ctx context.Context, conn remoteexec.Connection, path, realm string, body any) (remoteexec.Result, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return remoteexec.Result{}, err
	}
	parts := kcadmRealmArgs([]string{"delete", path}, realm)
	parts = append(parts, "-f", "-")
	return kcadmRunStdin(ctx, conn, string(b), parts...)
}

// kcadmErrMsg builds an error message from a nonzero kcadm.sh result,
// preferring stderr but falling back to stdout — the same preference
// keycloak_common.go's own kcadmFailedf applies, factored out here for
// callers in this batch that need the bare message (e.g. to wrap in a
// Go error) rather than a whole Fail() Result.
func kcadmErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// kcFindByField returns the first element of list whose field equals
// value, or nil if none matches — the client-side lookup this batch
// uses wherever the real module itself lists the full collection
// rather than relying on a server-side kcadm.sh `-q` filter (verified
// per-module against module_utils/_keycloak.py's own KeycloakAPI
// methods; e.g. get_clientscope_by_name's own loop over an unfiltered
// client-scopes list, since that collection has no server-side name
// filter, versus get_client_by_clientid's own server-side `?clientId=`
// filter for the clients collection).
func kcFindByField(list []map[string]any, field, value string) map[string]any {
	for _, item := range list {
		if s, ok := item[field].(string); ok && s == value {
			return item
		}
	}
	return nil
}

// kcString reads a string field from a decoded-JSON map, returning ""
// if absent or not a string.
func kcString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// kcJSONEqual compares a and b by their JSON-normalized form (both
// round-tripped through json.Marshal/Unmarshal into `any`) so that a Go
// int/[]string argument and the float64/[]any kcadm.sh's own JSON
// output decodes into never register as a false-positive diff.
func kcJSONEqual(a, b any) bool {
	ab, aerr := json.Marshal(a)
	bb, berr := json.Marshal(b)
	if aerr != nil || berr != nil {
		return reflect.DeepEqual(a, b)
	}
	var an, bn any
	if err := json.Unmarshal(ab, &an); err != nil {
		return false
	}
	if err := json.Unmarshal(bb, &bn); err != nil {
		return false
	}
	return reflect.DeepEqual(an, bn)
}

// kcMergeChangeset returns a shallow copy of existing with each key in
// changeset overwritten, plus whether any changeset key actually
// differs (via kcJSONEqual) from existing's current value for that
// key — the "fetch existing, merge proposed on top, PUT the merged
// whole" pattern real keycloak_* modules themselves use for an update
// (the Keycloak Admin REST API's PUT endpoints replace the whole
// resource representation, not a partial patch), used together with
// kcadmUpdateBody (a raw PUT of the exact body given — see
// keycloak_common.go's own doc comment on kcadmUpdateBody vs
// kcadmUpdate) so this batch's own modules send the merged whole, not
// just the changed fields.
func kcMergeChangeset(existing map[string]any, changeset map[string]any) (merged map[string]any, changed bool) {
	merged = make(map[string]any, len(existing)+len(changeset))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range changeset {
		if cur, ok := existing[k]; !ok || !kcJSONEqual(cur, v) {
			changed = true
		}
		merged[k] = v
	}
	return merged, changed
}

// kcSetIfPresent copies args[argKey] into changeset[jsonKey] only when
// argKey was actually given (matching every real keycloak_* module's
// own "unset argument means don't touch this field" convention —
// Ansible arguments default to None/absent, not an empty string).
func kcSetIfPresent(changeset map[string]any, args map[string]any, argKey, jsonKey string) {
	if v, ok := args[argKey]; ok {
		changeset[jsonKey] = v
	}
}

// asMapList converts a decoded-JSON/YAML []any (as found on a nested
// list-of-dicts argument, e.g. one execution's own nested
// authenticationExecutions) into a []map[string]any. Each element that
// isn't itself a map is dropped rather than causing an error, matching
// argStringList's own best-effort style.
func asMapList(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// argListOfMaps reads a module argument as a []map[string]any — used
// by this batch's own modules for a list-of-dicts argument
// (authenticationExecutions, protocol_mappers, required_actions, ...).
func argListOfMaps(args map[string]any, key string) []map[string]any {
	v, ok := args[key]
	if !ok {
		return nil
	}
	return asMapList(v)
}

// argRaw reads a module argument's raw value and whether it was given
// at all — used where a caller wants to forward an argument's value
// unchanged into a JSON request body (kcSetIfPresent's own single-key
// form) without narrowing it to a specific Go type first.
func argRaw(args map[string]any, key string) (any, bool) {
	v, ok := args[key]
	return v, ok
}

// kcResolveClientID resolves a client's own clientId (the human-chosen
// name, e.g. "myclient") to its internal Keycloak UUID, mirroring
// KeycloakAPI.get_client_id/get_client_by_clientid's own server-side
// `clients?clientId=` filter (verified: the `clients` collection is one
// of the few that supports a server-side name filter — see
// keycloak_common.go's own doc comment on when this batch uses `-q`
// versus a client-side kcFindByField loop). found=false (nil error)
// when no client has that clientId — an expected, common outcome for
// several of this batch's own modules' own argument validation (they
// fail with a clear message rather than surfacing a raw kcadm.sh
// error).
func kcResolveClientID(ctx context.Context, conn remoteexec.Connection, realm, clientID string) (id string, found bool, err error) {
	list, res, err := kcadmListMaps(ctx, conn, "clients", realm, []string{"clientId=" + clientID})
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, fmt.Errorf("looking up client %q in realm %s: %s", clientID, realm, kcadmErrMsg(res))
	}
	if m := kcFindByField(list, "clientId", clientID); m != nil {
		return kcString(m, "id"), true, nil
	}
	return "", false, nil
}

// kcadmListMaps runs `kcadm.sh get <path> [-r realm] (-q ...)*` (via
// keycloak_common.go's own kcadmGetJSON) and decodes the JSON array
// response into a []map[string]any, for a caller that will search it
// client-side with kcFindByField.
func kcadmListMaps(ctx context.Context, conn remoteexec.Connection, path, realm string, query []string) ([]map[string]any, remoteexec.Result, error) {
	var list []map[string]any
	res, err := kcadmGetJSON(ctx, conn, path, realm, query, &list)
	return list, res, err
}
