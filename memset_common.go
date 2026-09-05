package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the six memset_*.go modules in this batch
// (memset_dns_reload, memset_memstore_info, memset_server_info,
// memset_zone, memset_zone_domain, memset_zone_record) share: shelling
// out to Memset's own official `ma-shell` (Memset API Shell,
// github.com/memset/ma-shell) instead of real memset_*.py's own
// hand-rolled `memset_api_call` (module_utils/_memset.py — a bare
// `open_url` POST to `https://api.memset.com/v1/json/<method>/`). This
// is the same "shell out to the platform's own official CLI instead of
// an API client" precedent this port already uses elsewhere, and the
// same GENERIC API-passthrough shape hwc_common.go's own doc comment
// already establishes for KooCLI — `ma-shell`, like `hcloud`, has no
// resource-specific subcommand tree at all: every call is
// `<rpc-method-name> <param> <value> ...`, addressed by Memset's own
// published RPC method names (memset.com/apidocs), not a dedicated verb
// per resource.
//
// # ma-shell's own source (ma-shell.py, read in full before writing this
// # file, per this project's own bibliography-before-implementing rule)
//
// Confirmed directly from ma-shell's own (short, single-file) Python
// source, not guessed from its README or name:
//
//   - Non-interactive single-command mode is exactly
//     `ma-shell -k <API_KEY> <method> [<param_name> [(type)] <param_value> ...]`
//     — OptionParser's own positional `self.args`, when non-empty, is
//     handed straight to `execute(self.args, quiet=True)` and the
//     process returns immediately after printing the result, without
//     ever entering the interactive `raw_input` loop.
//   - Parameters are POSITIONAL "name value" pairs (NOT `name=value`),
//     optionally preceded by a bracketed type-cast token — `(string)`,
//     `(int)`, `(float)`, `(boolean)`, or `(list)` (comma-split) —
//     immediately before the value it applies to. A value with no cast
//     token is sent as a plain string.
//   - Output is `json.dumps(result, indent=2)` on stdout for a
//     successful call — no separate `-o json` flag needed, JSON is
//     ma-shell's own DEFAULT serialization (an `-x`/--xml flag switches
//     to XML instead, never used by this port).
//
// # A verified, real correctness bug in ma-shell itself: `(boolean)
// # false` does NOT work
//
// ma-shell's own type-cast code is `cast = bool` followed by
// `cmd[key+1] = cast(cmd[key+1])` — i.e. Python's bare `bool("false")`,
// which evaluates to `True` for ANY non-empty string, "false" included
// (only `bool("")` is `False` in Python, and an empty positional value
// cannot even be expressed via ma-shell's own shlex-style argv). There
// is NO way to send a literal boolean false through `(boolean)` at all
// — verified directly in ma-shell's own source, not inferred. This
// port therefore NEVER emits `(boolean) false`: a false-valued boolean
// argument (e.g. memset_zone_record's own `relative=false`, the default)
// is represented by OMITTING that parameter entirely and relying on
// Memset's own API-side default (confirmed, per each real memset_*.py
// module's own argument_spec, to already be `false`/`0` for every
// boolean/int field this batch uses) — never by attempting a cast this
// port has proven, from ma-shell's own source, cannot produce the value
// needed.
//
// # A verified, real quirk in how ma-shell reports failure: exit code 0
// # on an API-level fault
//
// ma-shell's own `execute()` catches `Fault`/`socket.error` around the
// RPC call itself and just `print`s "<method>: <exception>" to STDOUT
// (never stderr, never a non-zero `sys.exit`) — the process always
// exits 0 once the initial connection succeeds, REGARDLESS of whether
// the RPC call it was asked to make succeeded. Only a CONNECTION-time
// failure (a bad `-k` API key, unreachable API endpoint — both surfaced
// through `system.listMethods()`, ma-shell's own first network call,
// made once at startup before dispatching the requested command) goes
// through `OptionParser.error()`, which DOES `sys.exit(2)` with a
// message on stderr. So this port's own msCall (below) cannot use exit
// code alone to detect an API-level fault: a non-zero exit means
// "couldn't even connect / bad key" (this port surfaces res.Stderr);
// exit 0 with stdout that fails to parse as JSON means "the RPC call
// itself faulted, or the method name doesn't exist on the server" (this
// port surfaces the raw stdout line, which is exactly ma-shell's own
// "<method>: <error>" text); exit 0 with valid JSON stdout is success.
//
// # Auth: `-k` is UNAVOIDABLY on argv — verified, no alternative exists
//
// ma-shell's own source has NO environment-variable or config-file
// credential path anywhere at all (no `os.environ` lookup, no
// config-file reads of any kind in the entire file) — `-k`/`--api-key`
// is the ONLY way to supply the API key, always as a `--api-key`/`-k`
// command-line argument. This is a genuine, unavoidable deviation from
// this project's hard "no secrets in argv" rule, the same real,
// verified-from-source situation as rollbar_deployment.go's own
// `--access-token` (see that file's own doc comment for the identical
// reasoning) — not an oversight.
//
// # Transport note: XML-RPC here, JSON-over-HTTP in the real module —
// # same RPC namespace, verified against Memset's own published API docs
//
// Real memset_*.py's own `memset_api_call` POSTs form-encoded data to
// Memset's JSON-flavored HTTP endpoint
// (`https://api.memset.com/v1/json/<method>/`); `ma-shell` instead
// speaks XML-RPC (`https://api.memset.com/v1/xmlrpc`, verified directly
// in ma-shell's own source's `API_URL` constant) — two different WIRE
// ENCODINGS of the exact same RPC method namespace Memset documents once
// at memset.com/apidocs (method names, parameter names and semantics are
// shared across both bindings; only the transport/serialization
// differs), the same class of "different binding, same underlying
// operations" situation ibm_sa_common.go's own doc comment documents for
// pyxcli vs. xcli. Every RPC method name and parameter name this batch's
// six modules use below was read directly out of each real memset_*.py
// module's own source (module_utils/_memset.py's own memset_api_call
// call sites) before writing this port, per this project's own
// bibliography-before-implementing rule.
func msRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v ma-shell"); err != nil {
		return Fail(fmt.Sprintf("%s: the ma-shell binary (Memset's own official API Shell, github.com/memset/"+
			"ma-shell) is required on the target and was not found in PATH — this port shells out to it "+
			"rather than POSTing to Memset's JSON API directly; see memset_common.go's own doc comment",
			moduleName)), false
	}
	return Result{}, true
}

// msParam is one ma-shell positional "name [(type)] value" argument —
// see memset_common.go's own doc comment for the cast-type syntax and
// its one verified gap (boolean false).
type msParam struct {
	Name  string
	Type  string // "", "int", "float", "boolean", "list", "string"
	Value string
}

func msInt(name string, v int) msParam { return msParam{Name: name, Type: "int", Value: fmt.Sprint(v)} }
func msStr(name, v string) msParam     { return msParam{Name: name, Value: v} }
func msBoolTrue(name string) msParam   { return msParam{Name: name, Type: "boolean", Value: "true"} }
func msList(name string, vs []string) msParam {
	return msParam{Name: name, Type: "list", Value: strings.Join(vs, ",")}
}

// msCmd renders one `ma-shell -k <key> <method> ...` invocation.
func msCmd(apiKey, method string, params []msParam) string {
	parts := []string{"ma-shell", "-k", apiKey, method}
	for _, p := range params {
		if p.Type != "" {
			parts = append(parts, "("+p.Type+")")
		}
		parts = append(parts, p.Name, p.Value)
	}
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	return strings.Join(quoted, " ")
}

// msCall runs one ma-shell RPC call and returns its decoded JSON result.
// problem is non-empty (result nil) whenever the call did not succeed —
// see memset_common.go's own doc comment for why this can't be judged
// from the exit code alone: a non-zero exit means ma-shell couldn't even
// connect (problem is drawn from stderr); exit 0 with non-JSON stdout
// means the RPC call itself faulted (problem is ma-shell's own
// "<method>: <error>" stdout line verbatim). Every caller in this batch
// turns a non-empty problem into a Result{Failed:true}, never a Go
// error, matching this port's own "the request was well-formed, the
// platform refused it" convention.
func msCall(ctx context.Context, conn remoteexec.Connection, apiKey, method string, params []msParam) (result any, problem string, err error) {
	res, rerr := runStatus(ctx, conn, msCmd(apiKey, method, params))
	if rerr != nil {
		return nil, "", rerr
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return nil, msg, nil
	}
	trimmed := strings.TrimSpace(res.Stdout)
	var out any
	if jerr := json.Unmarshal([]byte(trimmed), &out); jerr != nil {
		return nil, trimmed, nil
	}
	return out, "", nil
}

// msArray converts an msCall result expected to be a JSON array of
// objects (every Memset "_list" method's own return shape) into
// []map[string]any, tolerating a nil/wrongly-shaped result by returning
// an empty slice rather than panicking — a defensive fallback, not an
// expected path, since a successful "_list" call's own shape is well
// documented and stable.
func msArray(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// msObject converts an msCall result expected to be a single JSON
// object into map[string]any, or nil if it wasn't one.
func msObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// msValidTTL reports whether ttl is one of the exact values every real
// memset_zone*.py module's own argument_spec restricts `ttl` to
// (verified in each module's own `choices=[...]` list, sourced from
// Memset's own dns.zone_create/dns.zone_record_create API docs).
func msValidTTL(ttl int) bool {
	switch ttl {
	case 0, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400:
		return true
	default:
		return false
	}
}
