package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the sixteen keycloak_* modules in this
// batch share: shelling out to `kcadm.sh` (Keycloak's own bundled
// admin CLI, shipped in every Keycloak/RH-SSO distribution under
// bin/kcadm.sh, and as kcadm.bat on Windows) instead of talking to the
// Keycloak Admin REST API through a hand-rolled `requests`-based HTTP
// client the way every real keycloak_* (community.general) module does
// (module_utils/_keycloak.py's KeycloakAPI class, formerly
// identity/keycloak/keycloak.py) — the same "shell out to the
// platform's own official CLI instead of an API client" precedent this
// port already uses for Consul, Redis, Terraform, Icinga2, Kopia,
// GitHub (github_common.go, via `gh`), and GitLab (gitlab_common.go,
// via `glab`) — a deliberate, user-approved architectural decision for
// this batch, not a gap.
//
// # `kcadm.sh` syntax verified
//
// kcadm.sh is a thin, GENERIC REST client over the Admin REST API, not
// a set of hand-written per-resource subcommands (with the narrow
// exception of the role-mapping convenience commands noted below): its
// core verbs are
//
//	kcadm.sh create <resource-path> [-r <realm>] (-s <field>=<value>)* [-f <file>|-] [-i]
//	kcadm.sh get    <resource-path> [-r <realm>] (-q <field>=<value>)* [--fields f1,f2]
//	kcadm.sh update <resource-path> [-r <realm>] (-s <field>=<value>)* (-d <field>)* [-f <file>|-]
//	kcadm.sh delete <resource-path> [-r <realm>]
//
// where <resource-path> is the Admin REST API path relative to
// `admin/realms/{realm}/` (e.g. `users`, `users/<id>`, `groups/<id>`,
// `components/<id>`) — EXCEPT realm objects themselves and a handful of
// other top-level resources, whose path is relative to `admin/` instead
// (e.g. `realms`, `realms/<name>`), which is why moduleKeycloakRealm
// never passes `-r` to kcadm at all. `-s field=value` sets one field of
// the JSON representation being built (create) or merged onto the
// fetched current representation before PUTing it back (update — see
// "update semantics" below); `-s parent.child=value` addresses a nested
// object field by dotted path, which is how this port sets fields
// inside `config` (e.g. `-s config.priority=["100"]`) — kcadm.sh's own
// documented VALUE-TYPE INFERENCE rule is: a `-s` value that parses as
// a JSON literal (a JSON string/number/bool/array/object) is sent as
// that JSON type; anything else is sent as a plain JSON string. This
// port always spells out an explicit JSON literal for non-string
// fields (kcadmSetBool/kcadmSetInt/kcadmSetJSON below) rather than
// relying on an ambiguous bare token, for a command line whose meaning
// does not depend on this inference rule's exact edge cases. `-i`
// (`--id-only`) prints just the newly created resource's ID (its own
// documented purpose: capturing an ID in a script, e.g.
// `id=$(kcadm.sh create users -r demo -s username=x -i)`), which this
// port relies on for every UUID-addressed resource (users, groups,
// components, identity providers report their own `alias` instead —
// see below) since `create`'s own default (non -i) stdout shape is not
// something this port could verify without a live server. `-f -` reads
// the FULL JSON body for the object being created/updated from stdin,
// bypassing `-s`/`-d` entirely — this port uses it (via kcadmCreateBody/
// kcadmUpdateBody below) for the handful of objects whose shape is
// awkward to build through repeated `-s parent.child=value` tokens (a
// dotted key that ITSELF contains literal dots, e.g.
// keycloak_userprofile's `kc.user.profile.config`; or a deeply nested
// mapper list).
//
// # `update` semantics: GET+merge+PUT vs raw PUT
//
// kcadm.sh's own documented behavior for `update` without `-f`/`-b` is
// to GET the resource's current representation, apply the `-s`/`-d`
// changes onto it in memory, then PUT the result back — a real,
// server-side-verified merge, not a client-side guess by this port.
// This is deliberately exploited by keycloak_realm_localization.go for
// its own `force=false` "only touch the listed keys" semantics (see
// that file's own doc comment): kcadm's own merge-on-update is exactly
// the "leave other keys alone" behavior that endpoint needs, no
// client-side diffing required. kcadmUpdateBody (below), by contrast,
// sends -f/-b instead of -s/-d — a raw PUT of the exact body given, no
// implicit GET-merge — used only where this port has ALREADY read the
// current representation itself and wants full control over exactly
// what gets sent (avoiding a second, redundant GET).
//
// # Role-mapping convenience commands
//
// Alongside the generic create/get/update/delete verbs, kcadm.sh also
// ships dedicated role-mapping commands — verified against Keycloak's
// own admin-cli documentation, not guessed from the module name:
//
//	kcadm.sh add-roles    (--uusername <u>|--uid <id>|--gname <g>|--gid <id>) [--cclientid <clientId>] --rolename <role> [-r <realm>]
//	kcadm.sh remove-roles (same flags)
//	kcadm.sh get-roles    (same target flags, no --rolename) [-r <realm>]
//
// omitting `--cclientid` maps/lists REALM-level roles; supplying it
// maps/lists that CLIENT's roles instead — exactly the branch every
// real keycloak_*_rolemapping module's own cid/client_id-vs-realm logic
// takes. keycloak_group.go/keycloak_realm_rolemapping.go/
// keycloak_user_rolemapping.go all use these instead of hand-rolling
// the underlying POST/DELETE .../role-mappings/{realm|clients/{id}}
// calls with `-f`.
//
// # Auth precondition (read this before touching any keycloak_*
// module's auth_keycloak_url/auth_realm/auth_username/auth_password/
// auth_client_id/auth_client_secret/token/refresh_token/
// validate_certs/connection_timeout/http_agent args)
//
// Every real keycloak_* module opens its own fresh OpenID-Connect
// session per task run (module_utils' KeycloakAPI, authenticating via
// auth_username/auth_password/auth_client_id/auth_client_secret, or a
// pre-obtained token/refresh_token). `kcadm.sh` has no equivalent
// per-invocation login: each of its commands talks to whatever
// server/realm/credentials are already cached in its own config file
// (`~/.keycloak/kcadm.config` by default, populated by a prior
// `kcadm.sh config credentials --server <url> --realm <realm> --user
// <user> --password <pass> --client <clientId>`) — exactly the same
// shape of narrowing ipa_common.go's own doc comment documents for
// `ipa` and a prior `kinit`, and gitlab_common.go's for `glab auth
// login`. `kcadm.sh config credentials` DOES accept `--password` as a
// literal argv token (or, if omitted, prompts interactively on a TTY)
// — but placing a password there from this port would violate this
// project's own hard "no secrets in argv" rule (see redis.go's own
// REDISCLI_AUTH precedent), and there is no environment-variable
// alternative kcadm.sh itself supports for it. So, matching the batch
// instructions' own framing exactly:
//   - auth_keycloak_url/auth_realm/auth_username/auth_password/
//     auth_client_id/auth_client_secret/token/refresh_token/
//     validate_certs/connection_timeout/http_agent are all accepted by
//     every keycloak_* module in this batch (for argument-shape
//     compatibility with real playbooks written against real
//     keycloak_* modules) but have NO EFFECT on this port's behavior —
//     never placed on an argv line or in an environment variable this
//     port sets. This is a deliberate, honestly-documented gap, not a
//     silent misinterpretation.
//   - A `kcadm.sh config credentials` session for a principal with
//     sufficient realm-management privileges to perform the requested
//     operation must already be authenticated on the target (that
//     command's own token cache auto-refreshes using a stored refresh
//     token, matching a long-lived FreeIPA Kerberos ticket's own
//     renewal story) before this port's keycloak_* modules run. A
//     playbook wanting distinct credentials per task can still run a
//     `command`/`shell` task invoking `kcadm.sh config credentials`
//     itself first — this port does not forbid that, it just does not
//     drive it FROM a keycloak_* module's own auth_* arguments.
//
// # Secrets inside a resource body (config.bindCredential,
// credentials[].value, config.clientSecret, ...)
//
// Unlike the connection-auth arguments above, a resource's own secret
// FIELDS (an LDAP bind password, a user's login credential, an OIDC
// client secret) are legitimate data this port DOES send — they are
// part of the object being created/updated, not a transport
// credential. They still never appear in a shell command line: every
// `-s field=value` token this port builds for a value considered
// sensitive is instead sent via kcadmCreateBody/kcadmUpdateBody's own
// `-f -` (piped over stdin, see above), never interpolated into the
// rendered command string itself. Values already redacted by Keycloak
// in its own read responses (`config.bindCredential`,
// `config.clientSecret`, keystore passwords — Keycloak returns
// `**********` for these on GET, matching every real keycloak_*
// module's own documented "cannot compare, always considered changed
// unless told otherwise" limitation) are handled the same way here,
// per-module, and documented at each call site.

// kcadmRequireBinary fails cleanly (Result{Failed:true}, not a Go
// error) if the real `kcadm.sh` CLI is not on the target's PATH.
func kcadmRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v kcadm.sh"); err != nil {
		return Fail(fmt.Sprintf("%s: the kcadm.sh binary (Keycloak's own bundled admin CLI) is required on the "+
			"target and was not found in PATH — this port shells out to it rather than speaking the Keycloak "+
			"Admin REST API directly; see keycloak_common.go's own doc comment, including the precondition "+
			"that `kcadm.sh config credentials` must already have been run on the target", moduleName)), false
	}
	return Result{}, true
}

// kcadmCmd renders one kcadm.sh invocation from already-meaningful
// parts, shell-quoting each.
func kcadmCmd(parts ...string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	return "kcadm.sh " + strings.Join(quoted, " ")
}

// kcadmRun runs one kcadm.sh invocation and returns its raw result (RC
// not treated as an error — callers decide what a nonzero exit means).
func kcadmRun(ctx context.Context, conn remoteexec.Connection, parts ...string) (remoteexec.Result, error) {
	return runStatus(ctx, conn, kcadmCmd(parts...))
}

// kcadmRunStdin is kcadmRun but pipes body to the command's stdin (used
// for `-f -`).
func kcadmRunStdin(ctx context.Context, conn remoteexec.Connection, body string, parts ...string) (remoteexec.Result, error) {
	return conn.Exec(ctx, kcadmCmd(parts...), strings.NewReader(body))
}

// kcadmRealmArgs appends "-r realm" unless realm is empty (realm
// objects themselves, and a few other top-level resources, are
// addressed with no realm scope at all — see keycloak_common.go's own
// doc comment).
func kcadmRealmArgs(parts []string, realm string) []string {
	if realm != "" {
		return append(parts, "-r", realm)
	}
	return parts
}

// kcadmGetJSON runs `kcadm.sh get <path> [-r realm] (-q ...)*` and, if
// it exits zero with non-empty stdout, decodes that stdout as JSON into
// out.
func kcadmGetJSON(ctx context.Context, conn remoteexec.Connection, path, realm string, query []string, out any) (remoteexec.Result, error) {
	parts := kcadmRealmArgs([]string{"get", path}, realm)
	for _, q := range query {
		parts = append(parts, "-q", q)
	}
	res, err := kcadmRun(ctx, conn, parts...)
	if err != nil {
		return res, err
	}
	if res.RC == 0 && out != nil && strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), out); jerr != nil {
			return res, fmt.Errorf("decoding kcadm get %s response: %w", path, jerr)
		}
	}
	return res, nil
}

// kcadmShow is kcadmGetJSON for a single-resource lookup by its own
// path (e.g. "users/<id>", "realms/<name>"): a nonzero exit is treated
// as "does not exist" (present=false, nil error) — matching
// ipa_common.go's own ipaShow convention exactly (a missing Keycloak
// object is an expected, common outcome, not an infrastructure
// failure).
func kcadmShow(ctx context.Context, conn remoteexec.Connection, path, realm string) (attrs map[string]any, present bool, err error) {
	var out map[string]any
	res, err := kcadmGetJSON(ctx, conn, path, realm, nil, &out)
	if err != nil {
		return nil, false, err
	}
	if res.RC != 0 {
		return nil, false, nil
	}
	return out, true, nil
}

// kcadmCreate runs `kcadm.sh create <path> [-r realm] <sets...>` (POST).
func kcadmCreate(ctx context.Context, conn remoteexec.Connection, path, realm string, sets []string) (remoteexec.Result, error) {
	parts := kcadmRealmArgs([]string{"create", path}, realm)
	parts = append(parts, sets...)
	return kcadmRun(ctx, conn, parts...)
}

// kcadmCreateID is kcadmCreate with `-i` (--id-only), returning the
// newly created resource's own ID (see keycloak_common.go's own doc
// comment on why this port relies on `-i` for every UUID-addressed
// resource).
func kcadmCreateID(ctx context.Context, conn remoteexec.Connection, path, realm string, sets []string) (id string, res remoteexec.Result, err error) {
	parts := kcadmRealmArgs([]string{"create", path}, realm)
	parts = append(parts, sets...)
	parts = append(parts, "-i")
	res, err = kcadmRun(ctx, conn, parts...)
	if err != nil || res.RC != 0 {
		return "", res, err
	}
	return strings.TrimSpace(res.Stdout), res, nil
}

// kcadmCreateBody runs `kcadm.sh create <path> [-r realm] -f -`,
// piping body's JSON encoding over stdin — see keycloak_common.go's own
// doc comment on when this port uses a raw body instead of `-s` tokens.
func kcadmCreateBody(ctx context.Context, conn remoteexec.Connection, path, realm string, body any) (remoteexec.Result, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return remoteexec.Result{}, err
	}
	parts := kcadmRealmArgs([]string{"create", path}, realm)
	parts = append(parts, "-f", "-")
	return kcadmRunStdin(ctx, conn, string(b), parts...)
}

// kcadmCreateBodyID is kcadmCreateBody with `-i`.
func kcadmCreateBodyID(ctx context.Context, conn remoteexec.Connection, path, realm string, body any) (id string, res remoteexec.Result, err error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", remoteexec.Result{}, err
	}
	parts := kcadmRealmArgs([]string{"create", path}, realm)
	parts = append(parts, "-f", "-", "-i")
	res, err = kcadmRunStdin(ctx, conn, string(b), parts...)
	if err != nil || res.RC != 0 {
		return "", res, err
	}
	return strings.TrimSpace(res.Stdout), res, nil
}

// kcadmUpdate runs `kcadm.sh update <path> [-r realm] <sets...>
// <deletes as -d ...>` — GET+merge+PUT, per kcadm's own documented
// generic update semantics (see keycloak_common.go's own doc comment).
func kcadmUpdate(ctx context.Context, conn remoteexec.Connection, path, realm string, sets, deletes []string) (remoteexec.Result, error) {
	parts := kcadmRealmArgs([]string{"update", path}, realm)
	parts = append(parts, sets...)
	for _, d := range deletes {
		parts = append(parts, "-d", d)
	}
	return kcadmRun(ctx, conn, parts...)
}

// kcadmUpdateBody runs `kcadm.sh update <path> [-r realm] -f -` — a raw
// PUT of body, bypassing kcadm's own GET-merge (see keycloak_common.go's
// own doc comment on when this port uses this instead of kcadmUpdate).
func kcadmUpdateBody(ctx context.Context, conn remoteexec.Connection, path, realm string, body any) (remoteexec.Result, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return remoteexec.Result{}, err
	}
	parts := kcadmRealmArgs([]string{"update", path}, realm)
	parts = append(parts, "-f", "-")
	return kcadmRunStdin(ctx, conn, string(b), parts...)
}

// kcadmDelete runs `kcadm.sh delete <path> [-r realm]`.
func kcadmDelete(ctx context.Context, conn remoteexec.Connection, path, realm string) (remoteexec.Result, error) {
	parts := kcadmRealmArgs([]string{"delete", path}, realm)
	return kcadmRun(ctx, conn, parts...)
}

// kcadmSet renders one `-s key=value` token pair for a plain string
// field.
func kcadmSet(key, value string) []string { return []string{"-s", key + "=" + value} }

// kcadmSetBool renders one `-s key=true|false` token pair — an
// explicit JSON boolean literal, per kcadm's own value-type inference
// (see keycloak_common.go's own doc comment).
func kcadmSetBool(key string, b bool) []string {
	return []string{"-s", key + "=" + strconv.FormatBool(b)}
}

// kcadmSetInt renders one `-s key=<n>` token pair — an explicit JSON
// number literal.
func kcadmSetInt(key string, n int) []string {
	return []string{"-s", key + "=" + strconv.Itoa(n)}
}

// kcadmSetJSON renders one `-s key=<json>` token pair, JSON-encoding v
// (used for list/dict-valued fields, and for a component's own
// MultivaluedHashMap `config.*` entries, which the Keycloak API
// requires as a JSON array of strings even for a conceptually-scalar
// value — e.g. `config.priority=["120"]`, never `config.priority=120`).
func kcadmSetJSON(key string, v any) ([]string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return []string{"-s", key + "=" + string(b)}, nil
}

// kcadmRoleTarget identifies who/what a role-mapping convenience
// command (add-roles/remove-roles/get-roles) applies to.
type kcadmRoleTarget struct {
	flag     string // one of --uid, --uusername, --gid, --gname
	value    string
	clientID string // optional --cclientid; empty means realm-level roles
}

func (t kcadmRoleTarget) argv() []string {
	out := []string{t.flag, t.value}
	if t.clientID != "" {
		out = append(out, "--cclientid", t.clientID)
	}
	return out
}

// kcadmAddRoles runs `kcadm.sh add-roles <target> --rolename <r> ...
// [-r realm]`.
func kcadmAddRoles(ctx context.Context, conn remoteexec.Connection, realm string, t kcadmRoleTarget, rolenames []string) (remoteexec.Result, error) {
	return kcadmRolesMutate(ctx, conn, "add-roles", realm, t, rolenames)
}

// kcadmRemoveRoles runs `kcadm.sh remove-roles <target> --rolename <r>
// ... [-r realm]`.
func kcadmRemoveRoles(ctx context.Context, conn remoteexec.Connection, realm string, t kcadmRoleTarget, rolenames []string) (remoteexec.Result, error) {
	return kcadmRolesMutate(ctx, conn, "remove-roles", realm, t, rolenames)
}

func kcadmRolesMutate(ctx context.Context, conn remoteexec.Connection, verb, realm string, t kcadmRoleTarget, rolenames []string) (remoteexec.Result, error) {
	parts := []string{verb}
	parts = append(parts, t.argv()...)
	for _, rn := range rolenames {
		parts = append(parts, "--rolename", rn)
	}
	parts = kcadmRealmArgs(parts, realm)
	return kcadmRun(ctx, conn, parts...)
}

// kcadmRoleRepr is one role representation as returned by
// `kcadm.sh get-roles`.
type kcadmRoleRepr struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ClientRole  bool   `json:"clientRole,omitempty"`
	ContainerID string `json:"containerId,omitempty"`
}

// kcadmGetRoles runs `kcadm.sh get-roles <target> [-r realm]` and
// decodes the resulting JSON array of role representations.
func kcadmGetRoles(ctx context.Context, conn remoteexec.Connection, realm string, t kcadmRoleTarget) ([]kcadmRoleRepr, remoteexec.Result, error) {
	parts := []string{"get-roles"}
	parts = append(parts, t.argv()...)
	parts = kcadmRealmArgs(parts, realm)
	var roles []kcadmRoleRepr
	res, err := kcadmGetJSONFromArgs(ctx, conn, parts, &roles)
	return roles, res, err
}

// kcadmGetJSONFromArgs is kcadmGetJSON for a command whose parts are
// already fully assembled (get-roles has no single "path" the way
// generic get/create/update/delete do).
func kcadmGetJSONFromArgs(ctx context.Context, conn remoteexec.Connection, parts []string, out any) (remoteexec.Result, error) {
	res, err := kcadmRun(ctx, conn, parts...)
	if err != nil {
		return res, err
	}
	if res.RC == 0 && out != nil && strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), out); jerr != nil {
			return res, fmt.Errorf("decoding kcadm %s response: %w", strings.Join(parts, " "), jerr)
		}
	}
	return res, nil
}

// kcadmFailedf builds a Fail() message from a nonzero kcadm.sh CLI
// result, preferring stderr but falling back to stdout.
func kcadmFailedf(moduleName, action string, res remoteexec.Result) Result {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return Fail(fmt.Sprintf("%s: %s: %s", moduleName, action, msg))
}

// normalizeAttributeValues coerces one Keycloak-style "attributes" dict
// value (a single scalar, or a list) into a []string — matching every
// real keycloak_group/keycloak_role's own doc: "Values may be single
// values (for example a string) or a list of strings."
func normalizeAttributeValues(v any) []string {
	switch x := v.(type) {
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			out[i] = fmt.Sprint(e)
		}
		return out
	case []string:
		return x
	default:
		return []string{fmt.Sprint(v)}
	}
}

// normalizeAttributes applies normalizeAttributeValues across a whole
// attributes dict.
func normalizeAttributes(m map[string]any) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = normalizeAttributeValues(v)
	}
	return out
}

// attributesEqual compares a desired (already normalized) attributes
// map against current's own raw JSON decoding (map[string]any whose
// values are []any of strings, or absent entirely).
func attributesEqual(want map[string][]string, current map[string]any) bool {
	haveRaw, _ := current["attributes"].(map[string]any)
	if len(want) != len(haveRaw) {
		return false
	}
	for k, wv := range want {
		hv, ok := haveRaw[k]
		if !ok {
			return false
		}
		if !stringSetEqual(wv, decodeStringSlice(hv)) {
			return false
		}
	}
	return true
}
