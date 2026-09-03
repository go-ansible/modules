package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRedisDataInfo implements Ansible's `redis_data_info`
// (community.general) module: read-only fetch of a single Redis key's
// value via `redis-cli GET` — see redis.go's own redisCli doc comment
// for why this port substitutes `redis-cli` for real redis_data_info's
// Python `redis` client library.
//
// Args: key (required); login_host, login_port, login_user,
// login_password, tls (default true, matching real redis_data_info),
// validate_certs, ca_certs, client_cert_file, client_key_file.
//
// Extra["exists"] (bool) and, only when the key exists, Extra["value"]
// — matching real redis_data_info's own return values exactly.
//
// Deviation from real redis_data_info: same raw-mode ambiguity as
// redis_data.go's own moduleRedisData — a key holding the empty string
// reads as absent through this port's `redis-cli GET`.
//
// Never Changed — this module only ever reads.
func moduleRedisDataInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	getRes, err := redisCli(ctx, conn, args, true, "GET", key)
	if err != nil {
		return Result{}, err
	}
	if getRes.RC != 0 {
		return Fail(fmt.Sprintf("redis_data_info: unable to get value of key %q: %s", key, strings.TrimSpace(getRes.Stderr))), nil
	}
	value := strings.TrimRight(getRes.Stdout, "\n")
	if value == "" {
		return Ok(fmt.Sprintf("Key %q does not exist in database", key)).WithExtra("exists", false), nil
	}
	return Ok(fmt.Sprintf("Got key %q", key)).WithExtra("exists", true).WithExtra("value", value), nil
}
