package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRedisInfo implements Ansible's `redis_info` (community.general)
// module: read-only Redis server/cluster facts via the `redis-cli` CLI's
// own `INFO`/`CLUSTER INFO` commands — see redis.go's own redisCli doc
// comment for why this port substitutes `redis-cli` for real
// redis_info's Python `redis` client library.
//
// Args: cluster (bool, default false) — also fetch `CLUSTER INFO`;
// login_host (default localhost), login_port (default 6379), login_user,
// login_password, tls (default false, matching real redis_info),
// validate_certs (default true), ca_certs, client_cert_file,
// client_key_file.
//
// Extra["info"]: every `key:value` line of `redis-cli INFO`'s default
// section set, as a map of strings. Extra["cluster_info"] (only when
// cluster=true): same shape, from `redis-cli CLUSTER INFO`.
//
// Deviation from real redis_info: real redis_info's Python client parses
// many of these fields into actual ints/floats (e.g. `connected_clients`
// as an int); this port returns every value as the raw string `INFO`'s
// text protocol contains, since redis-cli gives this port no structured
// reply to parse instead — a caller wanting a number converts it itself.
//
// Never Changed — this module only ever reads.
func moduleRedisInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	infoRes, err := redisCli(ctx, conn, args, false, "INFO")
	if err != nil {
		return Result{}, err
	}
	if infoRes.RC != 0 {
		return Fail("redis_info: unable to connect to database: " + strings.TrimSpace(infoRes.Stderr)), nil
	}
	res := Ok("").WithExtra("info", parseRedisInfoText(infoRes.Stdout))

	if argBool(args, "cluster", false) {
		clRes, err := redisCli(ctx, conn, args, false, "CLUSTER", "INFO")
		if err != nil {
			return Result{}, err
		}
		if clRes.RC != 0 {
			return Fail("redis_info: unable to read cluster info: " + strings.TrimSpace(clRes.Stderr)), nil
		}
		res = res.WithExtra("cluster_info", parseRedisInfoText(clRes.Stdout))
	}
	return res, nil
}

// parseRedisInfoText parses INFO/CLUSTER INFO's own `# Section` /
// `key:value` text protocol into a flat map, skipping section headers,
// comments, and blank lines — matching this port's general
// "flatten, don't nest by section" choice for read-only fact gathering
// (see systemd_info.go's own parseSystemdShow for the same shape of
// parse).
func parseRedisInfoText(out string) map[string]any {
	m := map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		m[k] = v
	}
	return m
}
