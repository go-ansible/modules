package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRedisData implements Ansible's `redis_data` (community.general)
// module: set/delete a single Redis key's value via the `redis-cli` CLI
// — see redis.go's own redisCli doc comment for why this port
// substitutes `redis-cli` for real redis_data's Python `redis` client
// library.
//
// Args: key (required); value (required when state=present); state
// (default present); existing (bool) / non_existing (bool) — mutually
// exclusive, map to Redis SET's own XX/NX flags; expiration (int,
// milliseconds) — maps to SET's PX flag; keep_ttl (bool) — maps to SET's
// KEEPTTL flag, mutually exclusive with expiration; login_host,
// login_port, login_user, login_password, tls (default TRUE, unlike
// redis.go/redis_info.go's own default false — matching real
// redis_data's own argspec exactly), validate_certs, ca_certs,
// client_cert_file, client_key_file.
//
// state=present is idempotent the same way real redis_data's own is: a
// `GET key` matching value, with keep_ttl not explicitly false and no
// expiration given, is a no-op; otherwise it always issues `SET` (with
// whatever XX/NX/PX/KEEPTTL flags were given) and reports Changed —
// matching real redis_data's own documented note that setting
// `expiration` "always results in a change in the database". If the SET
// reports a nil reply (its NX/XX condition wasn't met), this port fails
// cleanly, matching real redis_data's own fail_json for that case.
//
// state=absent deletes the key via `DEL`, Changed only if it existed.
//
// Deviation from real redis_data: redis-cli's raw output mode cannot
// distinguish a key holding the empty string from a key that does not
// exist at all (both print an empty line to stdout for `GET`); this port
// treats an empty GET reply as "key absent", which real redis_data's own
// redis-py client never confuses (it returns Python None only for a true
// cache miss, and the empty string is a distinct value). A key
// deliberately set to the empty string is therefore mishandled by this
// port's idempotency check.
func moduleRedisData(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")

	switch state {
	case "absent":
		delRes, err := redisCli(ctx, conn, args, true, "DEL", key)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("redis_data: unable to delete key: " + strings.TrimSpace(delRes.Stderr)), nil
		}
		if strings.TrimSpace(delRes.Stdout) == "0" {
			return Ok("Key: " + key + " not present"), nil
		}
		return Changed("Deleted key: " + key), nil

	case "present":
		value, err := requireString(args, "value")
		if err != nil {
			return Result{}, err
		}
		nx := argBool(args, "non_existing", false)
		xx := argBool(args, "existing", false)
		_, hasExpiration := args["expiration"]
		keepTTLRaw, keepTTLSet := args["keep_ttl"]
		_ = keepTTLRaw
		keepTTLFalse := keepTTLSet && !argBool(args, "keep_ttl", true)

		getRes, err := redisCli(ctx, conn, args, true, "GET", key)
		if err != nil {
			return Result{}, err
		}
		if getRes.RC != 0 {
			return Fail("redis_data: unable to get value of key: " + strings.TrimSpace(getRes.Stderr)), nil
		}
		old := strings.TrimRight(getRes.Stdout, "\n")

		if old == value && !hasExpiration && !keepTTLFalse {
			return Ok("Key "+key+" already has desired value").WithExtra("value", value).WithExtra("old_value", old), nil
		}

		cmdArgs := []string{"SET", key, value}
		if nx {
			cmdArgs = append(cmdArgs, "NX")
		}
		if xx {
			cmdArgs = append(cmdArgs, "XX")
		}
		if hasExpiration {
			cmdArgs = append(cmdArgs, "PX", strconv.Itoa(argInt(args, "expiration", 0)))
		}
		if argBool(args, "keep_ttl", false) {
			cmdArgs = append(cmdArgs, "KEEPTTL")
		}
		setRes, err := redisCli(ctx, conn, args, true, cmdArgs...)
		if err != nil {
			return Result{}, err
		}
		if setRes.RC != 0 {
			return Fail("redis_data: unable to set key: " + strings.TrimSpace(setRes.Stderr)), nil
		}
		if strings.TrimSpace(setRes.Stdout) == "" {
			if nx {
				return Fail(fmt.Sprintf("Could not set key: %s. Key already present.", key)), nil
			}
			return Fail(fmt.Sprintf("Could not set key: %s. Key not present.", key)), nil
		}
		return Changed("Set key: "+key).WithExtra("value", value).WithExtra("old_value", old), nil

	default:
		return Result{}, errArg("redis_data: state must be one of present, absent, got %q", state)
	}
}
