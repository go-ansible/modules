package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIp2locationInfo implements Ansible's `ip2location_info` module:
// gathers IP geolocation information for a host's IP address via
// IP2Location's own official `ip2locationio` CLI (repo
// `ip2location/ip2location-io-cli`) instead of real ip2location_info.py's
// own hand-rolled `fetch_url` call against the same keyless
// `https://api.ip2location.io/` endpoint — verified to be the exact same
// API both this CLI and the real module target, not a false-cognate
// unrelated product (IP2Location ships several distinct products; the
// real module's own doc comment explicitly names "the keyless
// U(api.ip2location.io) API").
//
// Args: ip (string, optional) — the IP address to look up; when omitted,
// both the real module and `ip2locationio` resolve the CALLER's own
// public IP instead. timeout/http_agent are accepted for argument-shape
// compatibility (matching real ip2location_info.py's own argument_spec)
// but have no effect, since `ip2locationio` sets its own user agent and
// has no per-invocation HTTP timeout flag of its own.
//
// # Auth
//
// None: both the real module and this CLI substitution use the fully
// keyless endpoint (real ip2location_info.py's own module docs state
// this explicitly, and never define an api_key/token argument at all) —
// this project's own no-secrets-in-argv rule is not implicated here,
// since there is no secret to place anywhere. `ip2locationio` does
// support an optional `-k <API_KEY>` flag for a higher-limit paid plan,
// but real ip2location_info.py has no argument to request that, so this
// port never passes it.
//
// Every field real ip2location_info.py's own RETURN documents
// (ip/country_code/country_name/region_name/city_name/latitude/
// longitude/zip_code/time_zone/asn/as/is_proxy) is a top-level key in
// `ip2locationio -o json`'s own response shape too, since both
// ultimately decode the same api.ip2location.io JSON response — so this
// port passes that decoded object straight through as Extra fields,
// exactly matching real ip2location_info.py's own
// `result.update(get_geo_data())` flattening.
func moduleIp2locationInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := run(ctx, conn, "command -v ip2locationio"); err != nil {
		return Fail("ip2location_info: the ip2locationio binary (IP2Location's own official CLI, " +
			"ip2location-io-cli) is required on the target and was not found in PATH — this port shells out to " +
			"it rather than speaking the keyless api.ip2location.io API directly; see this module's own doc " +
			"comment"), nil
	}

	cmd := "ip2locationio -o json"
	if ip := argString(args, "ip", ""); ip != "" {
		cmd += " " + shellQuote(ip)
	}

	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail(fmt.Sprintf("ip2location_info: ip2locationio failed: %s", msg)), nil
	}

	var fields map[string]any
	if strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), &fields); jerr != nil {
			return Result{}, fmt.Errorf("ip2location_info: decoding ip2locationio output: %w", jerr)
		}
	}
	return Result{Extra: fields}, nil
}
