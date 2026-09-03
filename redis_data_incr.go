package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// redisArgFloat reads a float64 module argument. This package's shared
// args.go has no such helper (only argInt/argBool/argString/...), and
// redis_data_incr.go is the only module in this batch needing a float
// argument, so it is kept local here rather than added to args.go, which
// this batch does not own.
func redisArgFloat(args map[string]any, key string, def float64) float64 {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err == nil {
			return f
		}
	}
	return def
}

// moduleRedisDataIncr implements Ansible's `redis_data_incr`
// (community.general) module: atomically increments a Redis key's
// integer or float value via `redis-cli`'s own INCR/INCRBY/INCRBYFLOAT
// commands — see redis.go's own redisCli doc comment for why this port
// substitutes `redis-cli` for real redis_data_incr's Python `redis`
// client library.
//
// Args: key (required); increment_int (int) / increment_float (float)
// — mutually exclusive; when neither is given, increments by 1 via
// `INCR` (matching real redis_data_incr's own documented default).
// login_host, login_port, login_user, login_password, tls (default
// true), validate_certs, ca_certs, client_cert_file, client_key_file.
//
// Extra["value"] (float64) — the new value after incrementing, matching
// real redis_data_incr's own `value` return (always returned as a float
// there too, even for an integer increment).
//
// Always Changed on success — an atomic increment always changes the
// key, matching real redis_data_incr's own unconditional behavior: it
// has no "would this be a no-op" check, since this port doesn't
// special-case an increment of exactly 0 either.
//
// Deviation from real redis_data_incr: real redis_data_incr's own
// check_mode support (partial: it simulates the new value by reading the
// key with GET and adding the increment locally, defaulting to 0.0 if
// absent, but requires the login_user to have GET permission) is not
// implemented — this port has no check-mode concept at all (see
// module.go's own doc comment: every module here always actually runs).
func moduleRedisDataIncr(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	_, hasInt := args["increment_int"]
	_, hasFloat := args["increment_float"]
	if hasInt && hasFloat {
		return Result{}, errArg("redis_data_incr: increment_int and increment_float are mutually exclusive")
	}

	var res remoteexec.Result
	var msg string
	switch {
	case hasFloat:
		inc := redisArgFloat(args, "increment_float", 0)
		incStr := strconv.FormatFloat(inc, 'f', -1, 64)
		res, err = redisCli(ctx, conn, args, true, "INCRBYFLOAT", key, incStr)
		msg = fmt.Sprintf("Incremented key: %s by %s to ", key, incStr)
	case hasInt:
		inc := argInt(args, "increment_int", 0)
		res, err = redisCli(ctx, conn, args, true, "INCRBY", key, strconv.Itoa(inc))
		msg = fmt.Sprintf("Incremented key: %s by %d to ", key, inc)
	default:
		res, err = redisCli(ctx, conn, args, true, "INCR", key)
		msg = fmt.Sprintf("Incremented key: %s to ", key)
	}
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("redis_data_incr: unable to increment key %q: %s", key, strings.TrimSpace(res.Stderr))), nil
	}
	out := strings.TrimSpace(res.Stdout)
	value, perr := strconv.ParseFloat(out, 64)
	if perr != nil {
		return Fail(fmt.Sprintf("redis_data_incr: value: %s of key: %s is not incrementable(int or float)", out, key)), nil
	}
	return Changed(msg+out).WithExtra("value", value), nil
}
