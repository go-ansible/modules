package modules

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulAgentService implements Ansible's `consul_agent_service`
// (community.general) module: creates, updates, or deletes a service
// registered with the local Consul agent via the `consul` CLI's own
// `consul services register`/`deregister` subcommands (see
// consul.go's own consulServicesRun, shared by both modules) — see
// consul_acl.go's own consulACLRun doc comment for why this port
// substitutes CLI calls for real consul_agent_service's python-consul/
// requests HTTP client generally. Unlike consul.go's own legacy
// `consul` module, this module never manages a check (matching real
// consul_agent_service's own documented scope: "there are currently no
// plans to create services and checks in one").
//
// Args: name (required to register); id (defaults to name); address;
// service_port (the API's own "Port"); tags ([]string); meta (dict);
// enable_tag_override (bool, default false); weights (dict{passing,
// warning}, default {passing:1, warning:1}); state (default present);
// host/port/scheme/ca_path/token (via CONSUL_HTTP_TOKEN)/
// validate_certs.
//
// Deviation from real consul_agent_service: real consul_agent_service
// reads the service's current state from the LOCAL agent's own
// `/v1/agent/services` endpoint; the `consul` CLI (per HashiCorp's own
// command reference) exposes no equivalent local-agent listing
// subcommand, only `consul catalog service <name>` (the cluster-wide
// catalog, populated from every agent's local state via anti-entropy —
// normally near-instant, but not the same read this port would ideally
// make). This port uses that catalog read for its own idempotency
// comparison instead, filtering its results by ServiceID.
//
// Changed compares address/service_port/tags(as a set)/meta/
// enable_tag_override/weights against the existing catalog entry; any
// difference (or the service not existing yet) writes a service
// definition JSON document to a target-side temp file and runs `consul
// services register <file>` (full replace, matching real
// consul_agent_service's own PUT-style register semantics — every
// managed field is always sent, not just the ones that changed).
// state=absent deregisters by ID (`consul services deregister -id
// <id>`) if the service currently exists, no-op otherwise.
func moduleConsulAgentService(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("consul_agent_service: state must be present or absent, got %q", state)
	}
	name := argString(args, "name", "")
	id := argString(args, "id", name)
	if id == "" {
		return Result{}, errArg("consul_agent_service: one of name or id is required")
	}
	lookupName := name
	if lookupName == "" {
		lookupName = id
	}

	existing, err := consulAgentServiceFind(ctx, conn, args, lookupName, id)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if existing == nil {
			return Ok("").WithExtra("service", nil), nil
		}
		res, err := consulServicesRun(ctx, conn, args, "deregister", []string{"-id", id})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("consul_agent_service: unable to deregister service " + id + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("").WithExtra("service", existing).WithExtra("operation", "delete"), nil
	}

	if name == "" {
		return Result{}, errArg("consul_agent_service: name is required to register a service")
	}

	desired := map[string]any{"id": id, "name": name}
	if a := argString(args, "address", ""); a != "" {
		desired["address"] = a
	}
	if p := argInt(args, "service_port", 0); p != 0 {
		desired["port"] = p
	}
	tags := argStringList(args, "tags")
	if len(tags) > 0 {
		desired["tags"] = tags
	}
	meta, _ := args["meta"].(map[string]any)
	if len(meta) > 0 {
		desired["meta"] = meta
	}
	enableTagOverride := argBool(args, "enable_tag_override", false)
	desired["enable_tag_override"] = enableTagOverride
	weights := consulAgentServiceWeights(args)
	desired["weights"] = weights

	if existing != nil && consulAgentServiceUnchanged(existing, args, tags, meta, enableTagOverride, weights) {
		return Ok("").WithExtra("service", existing), nil
	}

	def := map[string]any{"service": desired}
	b, err := json.Marshal(def)
	if err != nil {
		return Result{}, err
	}
	tmp := conn.TempPath("consul-agent-service.json")
	if _, err := conn.Exec(ctx, "cat > "+shellQuote(tmp), strings.NewReader(string(b))); err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.Remove(ctx, tmp) }()

	res, err := consulServicesRun(ctx, conn, args, "register", []string{tmp})
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("consul_agent_service: unable to register service " + name + ": " + strings.TrimSpace(res.Stderr)), nil
	}
	action := "create"
	if existing != nil {
		action = "update"
	}
	return Changed("").WithExtra("service", desired).WithExtra("operation", action), nil
}

// consulAgentServiceWeights reads the `weights` argument (default
// {passing:1, warning:1}, matching real consul_agent_service's own
// documented default), normalized to int.
func consulAgentServiceWeights(args map[string]any) map[string]any {
	w, _ := args["weights"].(map[string]any)
	passing, warning := 1, 1
	if w != nil {
		passing = argInt(w, "passing", 1)
		warning = argInt(w, "warning", 1)
	}
	return map[string]any{"passing": passing, "warning": warning}
}

// consulAgentServiceFind runs `consul catalog service <name>
// -format=json` and returns the entry whose ServiceID matches id, or
// nil if none does (including if the service doesn't exist in the
// catalog at all) — see this module's own doc comment for the
// catalog-vs-local-agent deviation this read represents.
func consulAgentServiceFind(ctx context.Context, conn remoteexec.Connection, args map[string]any, name, id string) (map[string]any, error) {
	opts := append([]string{"-format=json"}, consulConnArgs(args)...)
	all := append([]string{"catalog", "service", name}, opts...)
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = shellQuote(a)
	}
	cmd := "consul " + strings.Join(quoted, " ")
	if tok := argString(args, "token", ""); tok != "" {
		cmd = "CONSUL_HTTP_TOKEN=" + shellQuote(tok) + " " + cmd
	}
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &list); err != nil {
		return nil, nil
	}
	for _, e := range list {
		if consulACLStr(e, "ServiceID") == id {
			return e, nil
		}
	}
	return nil, nil
}

// consulAgentServiceUnchanged compares the catalog's own existing entry
// against the desired address/service_port/tags/meta/
// enable_tag_override/weights.
func consulAgentServiceUnchanged(existing map[string]any, args map[string]any, tags []string, meta map[string]any, enableTagOverride bool, weights map[string]any) bool {
	if argString(args, "address", "") != consulACLStr(existing, "ServiceAddress") {
		return false
	}
	if p := argInt(args, "service_port", 0); p != 0 {
		if existingPort, ok := existing["ServicePort"].(float64); !ok || int(existingPort) != p {
			return false
		}
	}
	var existingTags []string
	if raw, ok := existing["ServiceTags"].([]any); ok {
		for _, t := range raw {
			existingTags = append(existingTags, fmtString(t))
		}
	}
	if !consulACLStrSliceEqual(sortedCopy(tags), sortedCopy(existingTags)) {
		return false
	}
	existingMeta, _ := existing["ServiceMeta"].(map[string]any)
	// A nil (unset) desired meta and an empty {} existing meta both mean
	// "no metadata" — compare only when at least one side is non-empty,
	// so json.Marshal(nil)="null" doesn't spuriously mismatch
	// json.Marshal(map[string]any{})="{}".
	if (len(meta) != 0 || len(existingMeta) != 0) && !reflect.DeepEqual(jsonNormalizeAny(meta), jsonNormalizeAny(existingMeta)) {
		return false
	}
	if eto, ok := existing["ServiceEnableTagOverride"].(bool); !ok || eto != enableTagOverride {
		return false
	}
	existingWeights, _ := existing["ServiceWeights"].(map[string]any)
	// existing's own keys are capitalized (Passing/Warning); compare
	// against a re-keyed copy of desired rather than the raw (lowercase)
	// weights map.
	desiredWeights := map[string]any{"Passing": weights["passing"], "Warning": weights["warning"]}
	if !reflect.DeepEqual(jsonNormalizeAny(desiredWeights), jsonNormalizeAny(existingWeights)) {
		return false
	}
	return true
}

func fmtString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func sortedCopy(s []string) []string {
	out := append([]string{}, s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// jsonNormalizeAny round-trips v through JSON so differently-typed but
// equal values (Go int vs decoded float64, nil map vs empty map) compare
// equal via reflect.DeepEqual.
func jsonNormalizeAny(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}
