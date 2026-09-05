package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOvhIPFailover implements (a subset of) Ansible's
// `ovh_ip_failover` module via OVHcloud's own official `ovhcloud-cli` —
// see ovh_common.go's own doc comment for the CLI substitution
// rationale, credential wiring (application_key/application_secret/
// consumer_key/endpoint all genuinely map to OVH_* environment
// variables), and — critically for THIS module — the verified gap this
// doc comment expands on below.
//
// Args: name (string, required) — the failover IP (or block) to
// inspect; service (string, required) — the OVH service it should be
// routed to; endpoint/application_key/application_secret/consumer_key
// (all required, matching real ovh_ip_failover.py); timeout (int,
// default 120), wait_completion (bool, default true), and
// wait_task_completion (int, default 0) are all accepted for
// argument-shape compatibility but have NO EFFECT — see below.
//
// # Read works; the actual move does not — a verified CLI gap, not a
// # missing feature on this port's side
//
// `ovhcloud ip get <name>` IS a real, dedicated ovhcloud-cli verb
// (verified in ovhcloud-cli's own doc/ovhcloud_ip_get.md) and returns
// the IP's own `routedTo.serviceName` field, exactly the value real
// ovh_ip_failover.py's own `client.get(f"/ip/{name}")` reads to decide
// whether a move is even needed. This port uses it for the same
// purpose: when the IP is ALREADY routed to the requested service, this
// module reports Ok/unchanged, with no `ovhcloud` mutation attempted at
// all — the common case for a re-run against infrastructure that's
// already converged.
//
// But ovhcloud-cli's own `ip` command group has exactly five verbs —
// edit, firewall, get, list, reverse (verified directly against its own
// doc/ovhcloud_ip.md, and doc/ovhcloud_ip_edit.md's own flag list:
// `--description` only) — and NONE of them can move a failover IP
// between services. Real ovh_ip_failover.py's own core operation is
// `client.post(f"/ip/{name}/move", to=service)`; there is no `ovhcloud
// ip move` (or any other verb reaching that same REST endpoint), and
// (per ovh_common.go's own doc comment) no generic API-passthrough
// fallback either. So when a move IS actually needed, this module
// returns a Fail — not a Go error, since the request itself is entirely
// well-formed and a human operator hitting this exact wall from a
// terminal would see the identical absence of any command to run; it
// is this port's own architecture (shelling out to ovhcloud-cli
// specifically) that cannot satisfy it, honestly reported rather than
// silently skipped or faked as a no-op success.
func moduleOvhIPFailover(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := ovhRequireBinary(ctx, conn, "ovh_ip_failover"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, errArg("ovh_ip_failover: %v", err)
	}
	service, err := requireString(args, "service")
	if err != nil {
		return Result{}, errArg("ovh_ip_failover: %v", err)
	}
	env := ovhEnv(args)

	var ip map[string]any
	res, err := ovhRunJSON(ctx, conn, env, &ip, "ip", "get", name)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("ovh_ip_failover: IP %s does not exist or is not accessible: %s", name, ovhErrMsg(res))), nil
	}

	current := ""
	if routedTo, ok := ip["routedTo"].(map[string]any); ok {
		current = fmt.Sprint(routedTo["serviceName"])
	}
	if current == service {
		return Ok(fmt.Sprintf("%s already routed to %s", name, service)).WithExtra("moved", false), nil
	}

	return Fail(fmt.Sprintf("ovh_ip_failover: %s is routed to %s, not %s, and ovhcloud-cli has no command to move "+
		"a failover IP between services (verified: its own \"ip\" command group only has edit/firewall/get/"+
		"list/reverse, and there is no generic API-passthrough fallback either — see ovh_common.go's own doc "+
		"comment and ovh_ip_failover.go's own doc comment) — this move cannot be performed through this port",
		name, current, service)), nil
}
