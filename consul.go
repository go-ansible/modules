package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// consulServicesRun runs `consul services <action> <connArgs> <opts...>`
// on the target — shared by moduleConsul (this file) and
// moduleConsulAgentService (consul_agent_service.go), both of which
// register/deregister services with the local agent via the `consul`
// CLI's own `consul services` subcommand family. Token handling matches
// consul_acl.go's own consulACLRun: CONSUL_HTTP_TOKEN, not a -token
// flag.
func consulServicesRun(ctx context.Context, conn remoteexec.Connection, args map[string]any, action string, opts []string) (remoteexec.Result, error) {
	all := []string{"services", action}
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

// consulDurationSuffix appends Consul's own default "s" (seconds) unit
// suffix to a bare numeric duration string, matching every real
// consul/consul_agent_check doc's own documented convention ("`10' is
// `10s'"); a string that already ends in a letter (already has a unit)
// passes through unchanged.
func consulDurationSuffix(s string) string {
	if s == "" {
		return ""
	}
	last := s[len(s)-1]
	if last >= '0' && last <= '9' {
		return s + "s"
	}
	return s
}

// moduleConsul implements Ansible's `consul` (community.general)
// module: registers or deregisters a service (with an optional health
// check) — or a standalone node-level check — with a local Consul agent,
// via the `consul` CLI's own `consul services register`/`deregister`
// subcommands (there is no direct CLI equivalent of Consul's
// `/v1/agent/check/register` for a check that stands alone, unattached
// to any service — see the state=absent deviation note below) — see
// consul_acl.go's own consulACLRun doc comment for why this port
// substitutes CLI calls for real consul's python-consul HTTP client
// generally.
//
// Args: service_name/service_id (one of these registers a service;
// service_id defaults to service_name); service_port; service_address;
// tags ([]string); check_name/check_id/check_node (a standalone,
// node-level check when no service_name/service_id is given; check_id
// defaults to check_name); script (mutually exclusive with http/tcp/
// ttl, run as an Args array via this package's own tokenize, matching
// this port's command.go); http; tcp; ttl; interval (required with
// script/http/tcp); timeout; notes; state (default present); host
// (default localhost); port (default 8500); scheme (default http);
// token (via CONSUL_HTTP_TOKEN); validate_certs (default true).
//
// state=present builds a Consul agent service/check definition JSON
// document (embedding the check under the service when both are given,
// matching real consul's own combined registration; a bare "checks":
// [...] document with no "service" key for a standalone check) on a
// target-side temp file (conn.TempPath) and runs `consul services
// register <file>`. state=absent deregisters a service by ID (`consul
// services deregister -id <id>`) — deregistering a *standalone* check
// (no service_name/service_id at all) has no `consul` CLI equivalent at
// all (`consul services deregister` only operates on services; there is
// no `consul checks deregister` subcommand), so this port fails cleanly
// in that case rather than silently doing nothing.
//
// Changed: matching real consul's own explicitly documented limitation —
// "there is no complete way to retrieve the script, interval or TTL
// metadata for a registered check... this does not attempt to determine
// changes and it always reports a changed occurred" — this port applies
// that same always-changed behavior to every state=present call (not
// only ones with a check), as a deliberate simplification: distinguishing
// a check-bearing registration (genuinely always-changed, per real
// consul's own doc) from a pure service-only one (which
// consul_agent_service.go *does* make properly idempotent) would mean
// re-implementing consul_agent_service.go's own catalog-read comparison
// a second time in this legacy-compatibility module; callers wanting
// idempotent service-only registration should use consul_agent_service
// instead, matching real Ansible's own migration guidance toward the
// newer, more granular modules.
func moduleConsul(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("consul: state must be present or absent, got %q", state)
	}

	serviceName := argString(args, "service_name", "")
	serviceID := argString(args, "service_id", "")
	checkName := argString(args, "check_name", "")
	script := argString(args, "script", "")
	httpCheck := argString(args, "http", "")
	tcpCheck := argString(args, "tcp", "")
	ttl := argString(args, "ttl", "")

	hasService := serviceName != "" || serviceID != ""
	hasCheck := checkName != "" || script != "" || httpCheck != "" || tcpCheck != "" || ttl != ""
	if !hasService && !hasCheck {
		return Result{}, errArg("consul: one of service_name/service_id or check_name/script/http/tcp/ttl is required")
	}
	if (script != "" || httpCheck != "" || tcpCheck != "") && argString(args, "interval", "") == "" {
		return Result{}, errArg("consul: interval is required when script, http, or tcp is set")
	}

	if state == "absent" {
		if !hasService {
			return Fail("consul: removing a standalone node-level check (no service_name/service_id given) is not supported by this port — `consul services deregister` only deregisters services, and no `consul` CLI equivalent of `/v1/agent/check/deregister` exists"), nil
		}
		id := serviceID
		if id == "" {
			id = serviceName
		}
		res, err := consulServicesRun(ctx, conn, args, "deregister", []string{"-id", id})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("consul: unable to deregister service " + id + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("").WithExtra("service_id", id), nil
	}

	def := map[string]any{}
	if hasService {
		svc := map[string]any{"name": serviceName}
		if serviceID != "" {
			svc["id"] = serviceID
		}
		if p := argInt(args, "service_port", 0); p != 0 {
			svc["port"] = p
		}
		if a := argString(args, "service_address", ""); a != "" {
			svc["address"] = a
		}
		if tags := argStringList(args, "tags"); len(tags) > 0 {
			svc["tags"] = tags
		}
		if hasCheck {
			svc["checks"] = []any{consulCheckDef(args, false)}
		}
		def["service"] = svc
	} else {
		def["checks"] = []any{consulCheckDef(args, true)}
	}

	b, err := json.Marshal(def)
	if err != nil {
		return Result{}, err
	}
	tmp := conn.TempPath("consul-service.json")
	if _, err := conn.Exec(ctx, "cat > "+shellQuote(tmp), strings.NewReader(string(b))); err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.Remove(ctx, tmp) }()

	res, err := consulServicesRun(ctx, conn, args, "register", []string{tmp})
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("consul: unable to register: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Changed(""), nil
}

// consulCheckDef builds one Consul agent check definition object (the
// same JSON shape whether embedded under a service's own "checks" array
// or listed standalone at a definition file's top level) from
// check_id/check_name/check_node/script/http/tcp/ttl/interval/timeout/
// notes. standalone adds the "node" field (only meaningful, and only
// present in real consul's own args, for a node-level check with no
// service).
func consulCheckDef(args map[string]any, standalone bool) map[string]any {
	c := map[string]any{}
	if id := argString(args, "check_id", ""); id != "" {
		c["id"] = id
	}
	if name := argString(args, "check_name", ""); name != "" {
		c["name"] = name
	}
	if standalone {
		if node := argString(args, "check_node", ""); node != "" {
			c["node"] = node
		}
	}
	if script := argString(args, "script", ""); script != "" {
		c["args"] = tokenize(script)
	}
	if h := argString(args, "http", ""); h != "" {
		c["http"] = h
	}
	if tcp := argString(args, "tcp", ""); tcp != "" {
		c["tcp"] = tcp
	}
	if ttl := argString(args, "ttl", ""); ttl != "" {
		c["ttl"] = consulDurationSuffix(ttl)
	}
	if iv := argString(args, "interval", ""); iv != "" {
		c["interval"] = consulDurationSuffix(iv)
	}
	if to := argString(args, "timeout", ""); to != "" {
		c["timeout"] = consulDurationSuffix(to)
	}
	if n := argString(args, "notes", ""); n != "" {
		c["notes"] = n
	}
	return c
}
