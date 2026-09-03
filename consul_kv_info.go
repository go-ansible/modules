package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulKvInfo implements Ansible's `consul_kv_info`
// (community.general) module: read-only fetch of one or more Consul KV
// entries via the `consul` CLI's own `consul kv get -detailed` — see
// consul_kv.go's own consulKv doc comment for why this port substitutes
// the `consul` CLI for real consul_kv_info's python-consul HTTP client.
//
// Args: key (required, treated as a prefix when recurse=true); recurse
// (bool, default false); host (default localhost); port (default 8500);
// scheme (default http); datacenter; token (sent via the
// CONSUL_HTTP_TOKEN environment variable, not a CLI flag, keeping it out
// of the target's process listing — see consul_kv.go's own consulKv for
// the same choice); validate_certs (default true, mapped to
// `-tls-skip-verify` when false); ca_path.
//
// Extra["data"]: a list of maps (Key, Value, Flags, LockIndex, Session,
// CreateIndex, ModifyIndex — whichever fields `consul kv get -detailed`
// prints), one per matching key; an empty list if the key doesn't exist.
//
// Deviation from real consul_kv_info: real consul_kv_info calls
// Consul's HTTP API directly and returns Value base64-encoded exactly as
// Consul's API does, plus an `index` return value taken from the API
// response's `X-Consul-Index` header. This port's `consul` CLI
// substitution has no access to that header at all (no Extra["index"]
// is set), and returns Value as the CLI's own plain-text decoded value,
// never base64.
//
// Never Changed — this module only ever reads.
func moduleConsulKvInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	data, err := consulKvGetDetailed(ctx, conn, args, key)
	if err != nil {
		return Result{}, err
	}
	return Ok("").WithExtra("data", data), nil
}
