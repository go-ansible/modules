package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// consulConnArgs builds the `consul` CLI's own global HTTP-client flags
// (-http-addr/-datacenter/-ca-path/-tls-skip-verify) shared by every
// consul_kv* module in this port, matching the host/port/scheme/
// datacenter/ca_path/validate_certs options real consul_kv/
// consul_kv_info both document.
func consulConnArgs(args map[string]any) []string {
	scheme := argString(args, "scheme", "http")
	host := argString(args, "host", "localhost")
	port := argInt(args, "port", 8500)
	a := []string{"-http-addr=" + scheme + "://" + host + ":" + strconv.Itoa(port)}
	if dc := argString(args, "datacenter", ""); dc != "" {
		a = append(a, "-datacenter="+dc)
	}
	if ca := argString(args, "ca_path", ""); ca != "" {
		a = append(a, "-ca-path="+ca)
	}
	if !argBool(args, "validate_certs", true) {
		a = append(a, "-tls-skip-verify")
	}
	return a
}

// consulKv runs `consul kv <action> <connArgs> <opts...> <positional...>`
// on the target, passing the `token` argument (if set) via the
// CONSUL_HTTP_TOKEN environment variable rather than a `-token` CLI
// flag, keeping it out of the target's process listing — the same
// reasoning as redis.go's own redisCli using REDISCLI_AUTH; it is still
// embedded in the single shell command string handed to Connection.Exec
// (see module.go's own doc comment on that architectural limit).
//
// Deviation from real consul_kv/consul_kv_info: real modules speak
// Consul's HTTP API directly via python-consul; this port has no Go
// Consul client wired into remoteexec.Connection, so it substitutes
// shelling out to the `consul` CLI's own `consul kv` subcommand instead
// — same observable cluster-side effect, different transport, and (see
// each module's own doc comment) some HTTP-API-only details (the
// X-Consul-Index header, base64-encoded values) are not reproducible
// through a CLI at all.
func consulKv(ctx context.Context, conn remoteexec.Connection, args map[string]any, action string, opts []string, positional ...string) (remoteexec.Result, error) {
	all := []string{"kv", action}
	all = append(all, consulConnArgs(args)...)
	all = append(all, opts...)
	all = append(all, positional...)
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

// consulKvExistingValue reads a key's current plain-text value via
// `consul kv get <key>`, reporting exists=false (not an error) for a
// non-zero exit, matching real consul_kv's own kv_get returning None for
// a missing key rather than raising.
func consulKvExistingValue(ctx context.Context, conn remoteexec.Connection, args map[string]any, key string) (value string, exists bool, err error) {
	res, err := consulKv(ctx, conn, args, "get", nil, key)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	return strings.TrimRight(res.Stdout, "\n"), true, nil
}

// consulKvGetDetailed reads one or more entries via `consul kv get
// -detailed [-recurse] <key>`, returning one map per entry (empty, not
// an error, if the key doesn't exist) — used by both consul_kv.go's own
// state=present read path and consul_kv_info.go.
func consulKvGetDetailed(ctx context.Context, conn remoteexec.Connection, args map[string]any, key string) ([]map[string]any, error) {
	opts := []string{"-detailed"}
	if argBool(args, "recurse", false) {
		opts = append(opts, "-recurse")
	}
	res, err := consulKv(ctx, conn, args, "get", opts, key)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return []map[string]any{}, nil
	}
	return parseConsulKvDetailedBlocks(res.Stdout), nil
}

// parseConsulKvDetailedBlocks parses `consul kv get -detailed`'s own
// text table — one "Field   Value" line per attribute (CreateIndex,
// Flags, Key, LockIndex, ModifyIndex, Session, Value, ...), blocks for
// multiple keys (with -recurse) separated by a blank line. Each line is
// split on only its first run of whitespace so a Value field containing
// spaces is preserved intact.
func parseConsulKvDetailedBlocks(out string) []map[string]any {
	var blocks []map[string]any
	cur := map[string]any{}
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, cur)
			cur = map[string]any{}
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		cur[fields[0]] = strings.TrimSpace(fields[1])
	}
	flush()
	return blocks
}

// consulKvPut runs `consul kv put <opts...> <flags/cas from args>
// <key> <value>`, folding in the `flags`/`cas` module arguments common
// to every consulKv put in this file.
func consulKvPut(ctx context.Context, conn remoteexec.Connection, args map[string]any, key, value string, extraOpts ...string) (remoteexec.Result, error) {
	opts := append([]string{}, extraOpts...)
	if flags := argString(args, "flags", ""); flags != "" {
		opts = append(opts, "-flags="+flags)
	}
	if cas := argString(args, "cas", ""); cas != "" {
		opts = append(opts, "-cas", "-modify-index="+cas)
	}
	return consulKv(ctx, conn, args, "put", opts, key, value)
}

// moduleConsulKv implements Ansible's `consul_kv` (community.general)
// module: set, delete, or lock/unlock a key in Consul's KV store via the
// `consul` CLI's own `consul kv` subcommand — see consulKv's own doc
// comment for why this port substitutes the CLI for real consul_kv's
// python-consul HTTP client.
//
// Args: key (required); value (required for state=present's "set" path
// — omitting it falls back to a deprecated read, matching real
// consul_kv's own documented deprecation in favor of consul_kv_info);
// state (default present: present|absent|acquire|release); recurse
// (bool); retrieve (bool, default true — re-read after a successful
// set); session (required for acquire/release); cas, flags (opaque
// strings passed through to `consul kv put`'s own -cas/-modify-index/
// -flags); datacenter; host (default localhost); port (default 8500);
// scheme (default http); token (via CONSUL_HTTP_TOKEN, see consulKv);
// validate_certs (default true); ca_path.
//
// state=present with value set: idempotent on a `consul kv get`
// comparison against the existing value, matching real consul_kv's own
// `_has_value_changed`; issues `consul kv put` only when different.
// state=present with value omitted: read-only, Changed=false, matching
// real consul_kv's own deprecated get_value path.
// state=absent: Changed only if the key existed prior to `consul kv
// delete`.
// state=acquire/release: requires session; idempotent the same
// value-comparison way as the set path, then `consul kv put
// -acquire=<session>` or `-release=<session>`.
//
// Deviation from real consul_kv: real consul_kv's own `index` return
// value comes from Consul HTTP API's `X-Consul-Index` response header,
// which the `consul` CLI never exposes — this port does not set an
// Extra["index"] at all rather than fake one from `consul kv get
// -detailed`'s unrelated ModifyIndex field.
func moduleConsulKv(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")

	switch state {
	case "present":
		if _, hasValue := args["value"]; !hasValue {
			data, err := consulKvGetDetailed(ctx, conn, args, key)
			if err != nil {
				return Result{}, err
			}
			return Ok("").WithExtra("key", key).WithExtra("data", data), nil
		}
		value := argString(args, "value", "")
		old, exists, err := consulKvExistingValue(ctx, conn, args, key)
		if err != nil {
			return Result{}, err
		}
		changed := !exists || old != value
		if changed {
			putRes, err := consulKvPut(ctx, conn, args, key, value)
			if err != nil {
				return Result{}, err
			}
			if putRes.RC != 0 {
				return Fail("consul_kv: unable to set key " + key + ": " + strings.TrimSpace(putRes.Stderr)), nil
			}
		}
		var data any
		if argBool(args, "retrieve", true) {
			d, err := consulKvGetDetailed(ctx, conn, args, key)
			if err != nil {
				return Result{}, err
			}
			data = d
		}
		res := Result{Changed: changed}
		return res.WithExtra("key", key).WithExtra("data", data), nil

	case "absent":
		opts := []string{}
		if argBool(args, "recurse", false) {
			opts = append(opts, "-recurse")
		}
		data, err := consulKvGetDetailed(ctx, conn, args, key)
		if err != nil {
			return Result{}, err
		}
		changed := len(data) > 0
		if changed {
			delRes, err := consulKv(ctx, conn, args, "delete", opts, key)
			if err != nil {
				return Result{}, err
			}
			if delRes.RC != 0 {
				return Fail("consul_kv: unable to delete key " + key + ": " + strings.TrimSpace(delRes.Stderr)), nil
			}
		}
		res := Result{Changed: changed}
		return res.WithExtra("key", key).WithExtra("data", data), nil

	case "acquire", "release":
		session := argString(args, "session", "")
		if session == "" {
			return Fail(state + " of lock for " + key + " requested but no session supplied"), nil
		}
		value := argString(args, "value", "")
		old, exists, err := consulKvExistingValue(ctx, conn, args, key)
		if err != nil {
			return Result{}, err
		}
		changed := !exists || old != value
		if changed {
			lockOpt := "-acquire=" + session
			if state == "release" {
				lockOpt = "-release=" + session
			}
			putRes, err := consulKvPut(ctx, conn, args, key, value, lockOpt)
			if err != nil {
				return Result{}, err
			}
			changed = putRes.RC == 0
		}
		res := Result{Changed: changed}
		return res.WithExtra("key", key), nil

	default:
		return Result{}, errArg("consul_kv: state must be one of present, absent, acquire, release, got %q", state)
	}
}
