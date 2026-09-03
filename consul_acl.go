package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// consulACLRun runs `consul acl <resource> <action> <connArgs> <opts...>`
// on the target, reusing consulConnArgs (consul_kv.go's own shared
// -http-addr/-datacenter/-ca-path/-tls-skip-verify builder — it is
// already generic over any consul module's host/port/scheme/datacenter/
// ca_path/validate_certs args, not KV-specific, so this file adds no
// duplicate of it) and passing `token` via the CONSUL_HTTP_TOKEN
// environment variable rather than a `-token` flag, the same reasoning
// consulKv documents for keeping a credential out of the target's
// process listing.
//
// Deviation shared by every consul_acl_*.go module in this port: real
// consul_policy/consul_role/consul_token/consul_auth_method/
// consul_binding_rule speak Consul's HTTP API directly via python-consul
// (or requests); this port has no Go Consul client wired into
// remoteexec.Connection, so it substitutes shelling out to the `consul`
// CLI's own `consul acl <resource>` subcommands instead — same
// observable cluster-side effect, different transport.
func consulACLRun(ctx context.Context, conn remoteexec.Connection, args map[string]any, resource, action string, opts []string) (remoteexec.Result, error) {
	all := []string{"acl", resource, action}
	all = append(all, consulConnArgs(args)...)
	all = append(all, opts...)
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = shellQuote(a)
	}
	cmd := "consul " + strings.Join(quoted, " ")
	if tok := argString(args, "token", ""); tok != "" {
		cmd = "CONSUL_HTTP_TOKEN=" + shellQuote(tok) + " " + cmd
	}
	return conn.Exec(ctx, cmd, nil)
}

// consulACLReadMap runs a `consul acl <resource> read/create/update
// -format=json`-shaped command and JSON-decodes a single object from
// stdout. exists is false (not an error) for a non-zero exit, matching
// this package's other consul helpers (e.g. consul_kv.go's own
// consulKvExistingValue) treating "not found" as a normal, expected
// outcome rather than a Go error.
func consulACLReadMap(ctx context.Context, conn remoteexec.Connection, args map[string]any, resource, action string, opts []string) (obj map[string]any, exists bool, err error) {
	res, err := consulACLRun(ctx, conn, args, resource, action, consulACLWithFormatJSON(opts))
	if err != nil {
		return nil, false, err
	}
	if res.RC != 0 {
		return nil, false, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &m); err != nil {
		return nil, false, fmt.Errorf("consul acl %s %s: decoding JSON output: %w", resource, action, err)
	}
	return m, true, nil
}

// consulACLReadList is consulACLReadMap for a `list` action, whose
// output is a JSON array rather than a single object.
func consulACLReadList(ctx context.Context, conn remoteexec.Connection, args map[string]any, resource string, opts []string) ([]map[string]any, error) {
	res, err := consulACLRun(ctx, conn, args, resource, "list", consulACLWithFormatJSON(opts))
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("consul acl %s list: %s", resource, strings.TrimSpace(res.Stderr))
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &list); err != nil {
		return nil, fmt.Errorf("consul acl %s list: decoding JSON output: %w", resource, err)
	}
	return list, nil
}

// consulACLWithFormatJSON returns a copy of opts with a trailing
// "-format=json" appended, shared by consulACLReadMap/consulACLReadList
// so every JSON-decoding caller gets it automatically rather than each
// having to remember it themselves (a plain consulACLRun call, e.g. for
// `delete`, whose output this package never decodes, does not go
// through this helper and gets no such flag).
func consulACLWithFormatJSON(opts []string) []string {
	out := make([]string, 0, len(opts)+1)
	out = append(out, opts...)
	return append(out, "-format=json")
}

// consulACLStr reads a string field from a JSON-decoded ACL object,
// tolerating a missing key (returns "").
func consulACLStr(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	if v, ok := obj[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// consulACLRefKeys builds a sorted, coarse-grained identity fingerprint
// for a list of policy/role/{service,node}-identity references — an
// "id:<ID>" or "name:<Name>" token per entry, preferring name when both
// are present. It is used to compare the *set* of attached
// policies/roles/identities between this port's desired args and the
// Consul API's own existing object, without needing to resolve a
// name-only reference given in args to the ID the API always returns
// (this port has no separate lookup round-trip for that) — a deliberate
// simplification documented on each caller: two entries that name the
// same policy once by name and once by bare ID would compare unequal
// here, causing a spurious Changed and a redundant (idempotent, no-op on
// the Consul side) update call.
func consulACLRefKeys(entries []map[string]any, idKey, nameKey string) []string {
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if n := consulACLStr(e, nameKey); n != "" {
			keys = append(keys, "name:"+n)
			continue
		}
		if id := consulACLStr(e, idKey); id != "" {
			keys = append(keys, "id:"+id)
		}
	}
	sort.Strings(keys)
	return keys
}

// consulACLStrSliceEqual compares two string slices as sets (order-
// independent, matching consulACLRefKeys's own sorted output).
func consulACLStrSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// consulServiceIdentityOpts builds repeated `-service-identity=NAME
// [:DC1,DC2,...]` flags from a `service_identities` list of
// {service_name (alias name), datacenters} maps — the shape shared
// verbatim by consul_role and consul_token's own suboptions.
func consulServiceIdentityOpts(entries []map[string]any) []string {
	var opts []string
	for _, e := range entries {
		name := argString(e, "service_name", argString(e, "name", ""))
		if name == "" {
			continue
		}
		v := name
		if dcs := argStringList(e, "datacenters"); len(dcs) > 0 {
			v += ":" + strings.Join(dcs, ",")
		}
		opts = append(opts, "-service-identity", v)
	}
	return opts
}

// consulNodeIdentityOpts builds repeated `-node-identity=NAME:DATACENTER`
// flags from a `node_identities` list of {node_name (alias name),
// datacenter} maps — the shape shared verbatim by consul_role and
// consul_token's own suboptions.
func consulNodeIdentityOpts(entries []map[string]any) []string {
	var opts []string
	for _, e := range entries {
		name := argString(e, "node_name", argString(e, "name", ""))
		dc := argString(e, "datacenter", "")
		if name == "" {
			continue
		}
		v := name
		if dc != "" {
			v += ":" + dc
		}
		opts = append(opts, "-node-identity", v)
	}
	return opts
}

// consulPolicyRefOpts builds repeated -policy-id/-policy-name flags from
// a `policies` list of {id,name} maps (the shape shared verbatim by
// consul_role and consul_token's own `policies` suboption).
func consulPolicyRefOpts(entries []map[string]any) []string {
	var opts []string
	for _, e := range entries {
		if id := argString(e, "id", ""); id != "" {
			opts = append(opts, "-policy-id", id)
		} else if name := argString(e, "name", ""); name != "" {
			opts = append(opts, "-policy-name", name)
		}
	}
	return opts
}

// consulRoleRefOpts builds repeated -role-id/-role-name flags from a
// `roles` list of {id,name} maps (consul_token's own `roles`
// suboption).
func consulRoleRefOpts(entries []map[string]any) []string {
	var opts []string
	for _, e := range entries {
		if id := argString(e, "id", ""); id != "" {
			opts = append(opts, "-role-id", id)
		} else if name := argString(e, "name", ""); name != "" {
			opts = append(opts, "-role-name", name)
		}
	}
	return opts
}

// consulServiceIdentityTokens builds a sorted "name|dc1,dc2" fingerprint
// per entry for comparing a desired vs existing service_identities list
// as sets — entries are pre-normalized to the API's own ServiceName/
// Datacenters keys by the caller (consulServiceIdentityArgsToAPI for the
// args side; the API's own JSON response needs no normalization).
func consulServiceIdentityTokens(entries []map[string]any) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := consulACLStr(e, "ServiceName")
		var dcs []string
		if raw, ok := e["Datacenters"].([]any); ok {
			for _, d := range raw {
				dcs = append(dcs, fmt.Sprint(d))
			}
		}
		sort.Strings(dcs)
		out = append(out, name+"|"+strings.Join(dcs, ","))
	}
	sort.Strings(out)
	return out
}

// consulNodeIdentityTokens is consulServiceIdentityTokens for
// node_identities ("name|datacenter" per entry).
func consulNodeIdentityTokens(entries []map[string]any) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, consulACLStr(e, "NodeName")+"|"+consulACLStr(e, "Datacenter"))
	}
	sort.Strings(out)
	return out
}

// consulServiceIdentityArgsToAPI normalizes a `service_identities` args
// list (service_name/name alias, datacenters) to the API's own
// ServiceName/Datacenters keys, for use with consulServiceIdentityTokens.
func consulServiceIdentityArgsToAPI(entries []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		name := argString(e, "service_name", argString(e, "name", ""))
		var dcs []any
		for _, d := range argStringList(e, "datacenters") {
			dcs = append(dcs, d)
		}
		out = append(out, map[string]any{"ServiceName": name, "Datacenters": dcs})
	}
	return out
}

// consulNodeIdentityArgsToAPI is consulServiceIdentityArgsToAPI for
// node_identities (node_name/name alias, datacenter).
func consulNodeIdentityArgsToAPI(entries []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		name := argString(e, "node_name", argString(e, "name", ""))
		out = append(out, map[string]any{"NodeName": name, "Datacenter": argString(e, "datacenter", "")})
	}
	return out
}

// consulExistingList reads a []any field of an ACL API object (e.g.
// existing["Policies"]) as []map[string]any, tolerating a missing or
// nil field.
func consulExistingList(existing map[string]any, key string) []map[string]any {
	raw, ok := existing[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// consulACLMapList reads a module argument as a []map[string]any (a
// list of dicts, e.g. `policies`/`roles`/`service_identities`), the
// shape argStringList/argMapAny don't cover.
func consulACLMapList(args map[string]any, key string) []map[string]any {
	v, ok := args[key]
	if !ok {
		return nil
	}
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
