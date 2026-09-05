package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOvhIPLoadbalancingBackend implements (a subset of) Ansible's
// `ovh_ip_loadbalancing_backend` module via OVHcloud's own official
// `ovhcloud-cli` — see ovh_common.go's own doc comment for the CLI
// substitution rationale, credential wiring, and — critically for THIS
// module — the verified gap this doc comment expands on below.
//
// Args: name (string, required) — the LoadBalancing's own internal name
// (`ip-X.X.X.X`); backend (string, required) — the backend IP; state
// (present|absent, default "present"); probe (none|http|icmp|oco,
// default "none"); weight (int, default 8); endpoint/application_key/
// application_secret/consumer_key (all required, matching real
// ovh_ip_loadbalancing_backend.py); timeout (int, default 120) is
// accepted for argument-shape compatibility but has no effect.
//
// # The LoadBalancing service itself is inspectable; its backends are
// # not reachable through ovhcloud-cli AT ALL — a verified CLI gap
//
// `ovhcloud iploadbalancing get <name>` IS a real, dedicated
// ovhcloud-cli verb (verified in ovhcloud-cli's own doc/
// ovhcloud_iploadbalancing_get.md), so this module DOES confirm the
// named LoadBalancing service exists before going any further — a Fail
// with the same "IP LoadBalancing X does not exist" wording real
// ovh_ip_loadbalancing_backend.py itself uses when it doesn't.
//
// But ovhcloud-cli's own `iploadbalancing` command group has exactly
// three verbs — edit, get, list (verified directly against its own
// doc/ovhcloud_iploadbalancing.md) — and none of them reaches any
// BACKEND sub-resource at all: there is no `ovhcloud iploadbalancing
// backend` command group of any kind (create/get/list/update/delete),
// unlike, say, `ovhcloud ip firewall rule`'s own nested sub-resource
// pattern elsewhere in the same CLI. Real
// ovh_ip_loadbalancing_backend.py's own every operation —
// `GET/POST/PUT/DELETE /ip/loadBalancing/{name}/backend[/{backend}]` —
// has no ovhcloud-cli verb reaching it, and (per ovh_common.go's own
// doc comment) no generic API-passthrough fallback either. So, once the
// LoadBalancing service itself is confirmed to exist, this module
// always returns a Fail for the actual backend create/update/delete
// request — not a Go error, since the request is entirely well-formed
// and a human operator would hit the identical absence of any command
// to run; it is this port's own architecture (shelling out to
// ovhcloud-cli specifically) that cannot satisfy it, honestly reported
// rather than silently skipped or faked as a no-op success.
func moduleOvhIPLoadbalancingBackend(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := ovhRequireBinary(ctx, conn, "ovh_ip_loadbalancing_backend"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, errArg("ovh_ip_loadbalancing_backend: %v", err)
	}
	backend, err := requireString(args, "backend")
	if err != nil {
		return Result{}, errArg("ovh_ip_loadbalancing_backend: %v", err)
	}
	env := ovhEnv(args)

	res, err := ovhRun(ctx, conn, env, "iploadbalancing", "get", name)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("ovh_ip_loadbalancing_backend: IP LoadBalancing %s does not exist: %s", name, ovhErrMsg(res))), nil
	}

	return Fail(fmt.Sprintf("ovh_ip_loadbalancing_backend: LoadBalancing %s exists, but ovhcloud-cli has no "+
		"command reaching backend %s at all (verified: its \"iploadbalancing\" command group only has "+
		"edit/get/list — no backend sub-resource of any kind — and there is no generic API-passthrough "+
		"fallback either — see ovh_common.go's own doc comment and ovh_ip_loadbalancing_backend.go's own doc "+
		"comment) — this request cannot be performed through this port", name, backend)), nil
}
