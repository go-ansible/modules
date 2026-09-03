package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulAgentCheck implements Ansible's `consul_agent_check`
// (community.general) module: registers, updates, or deregisters a
// health check with the local Consul agent — via the `consul` CLI's own
// `consul services register`/`deregister` subcommands, the only `consul`
// CLI family this port found with any local-agent check-registration
// capability at all (Consul agent config files, and by extension
// `consul services register <file>`, accept a standalone top-level
// "checks" array with no "service" key — the same shape a
// `/etc/consul.d/*.json` snippet uses to define a node-level check; see
// consul.go's own consulServicesRun) — see consul_acl.go's own
// consulACLRun doc comment for why this port substitutes CLI calls for
// real consul_agent_check's python-consul/requests HTTP client
// generally.
//
// Args: name (required); id (defaults to name); args ([]string,
// mutually exclusive with http/tcp/ttl); http; tcp; ttl; interval
// (required with args/http/tcp); timeout; notes; service_id (attaches
// the check to an already-registered service); state (default present);
// host/port/scheme/ca_path/token (via CONSUL_HTTP_TOKEN)/
// validate_certs.
//
// Changed: matching real consul_agent_check's own explicitly documented
// limitation — "there is no complete way to retrieve the script,
// interval or TTL metadata for a registered check... the module does
// not attempt to determine changes and it always reports a changed
// occurred" — state=present here ALWAYS reports Changed=true, exactly
// as real consul_agent_check itself does; there is no idempotency gap
// specific to this port's own CLI substitution.
//
// Deviation from real consul_agent_check for state=absent: Consul's
// real `/v1/agent/check/deregister/:check_id` HTTP endpoint has no
// dedicated `consul` CLI subcommand at all (only `consul services
// deregister`, which HashiCorp's own command reference documents as
// deregistering a service by ID, not a bare check). This port's
// best-effort substitution re-submits the SAME standalone check
// definition file used for registration to `consul services deregister
// <file>` (rather than `-id`), mirroring how `consul services register
// <file>` accepts a checks-only file for creation — this is inferred
// from that symmetry, not confirmed against HashiCorp's own
// documentation (which does not describe FILE-argument deregister
// behavior for a checks-only definition), so state=absent's own success
// here should be treated as unverified rather than a documented
// guarantee.
func moduleConsulAgentCheck(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("consul_agent_check: state must be present or absent, got %q", state)
	}
	id := argString(args, "id", name)
	scriptArgs := argStringList(args, "args")
	httpCheck := argString(args, "http", "")
	tcpCheck := argString(args, "tcp", "")
	ttl := argString(args, "ttl", "")
	if (len(scriptArgs) > 0 || httpCheck != "" || tcpCheck != "") && argString(args, "interval", "") == "" {
		return Result{}, errArg("consul_agent_check: interval is required with args, http, or tcp")
	}

	check := map[string]any{"id": id, "name": name}
	if sid := argString(args, "service_id", ""); sid != "" {
		check["serviceid"] = sid
	}
	if len(scriptArgs) > 0 {
		check["args"] = scriptArgs
	}
	if httpCheck != "" {
		check["http"] = httpCheck
	}
	if tcpCheck != "" {
		check["tcp"] = tcpCheck
	}
	if ttl != "" {
		check["ttl"] = consulDurationSuffix(ttl)
	}
	if iv := argString(args, "interval", ""); iv != "" {
		check["interval"] = consulDurationSuffix(iv)
	}
	if to := argString(args, "timeout", ""); to != "" {
		check["timeout"] = consulDurationSuffix(to)
	}
	if n := argString(args, "notes", ""); n != "" {
		check["notes"] = n
	}

	def := map[string]any{"checks": []any{check}}
	b, err := json.Marshal(def)
	if err != nil {
		return Result{}, err
	}
	tmp := conn.TempPath("consul-agent-check.json")
	if _, err := conn.Exec(ctx, "cat > "+shellQuote(tmp), strings.NewReader(string(b))); err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.Remove(ctx, tmp) }()

	action := "register"
	if state == "absent" {
		action = "deregister"
	}
	res, err := consulServicesRun(ctx, conn, args, action, []string{tmp})
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("consul_agent_check: unable to " + action + " check " + id + ": " + strings.TrimSpace(res.Stderr)), nil
	}
	if state == "absent" {
		return Changed("").WithExtra("check", check).WithExtra("operation", "delete"), nil
	}
	return Changed("").WithExtra("check", check).WithExtra("operation", "update"), nil
}
