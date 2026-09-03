package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// redisConnArgs builds the redis-cli connection flags (-h/-p/--user/
// --tls/--cacert/--cert/--key/--insecure) shared by every redis_* module
// in this port, matching the login_host/login_port/login_user/tls/
// ca_certs/client_cert_file/client_key_file/validate_certs options every
// real redis/redis_* module documents identically. tlsDefault is passed
// explicitly because real redis.py/redis_info.py default tls=false while
// redis_data.py/redis_data_info.py/redis_data_incr.py default tls=true.
func redisConnArgs(args map[string]any, tlsDefault bool) []string {
	a := []string{
		"-h", argString(args, "login_host", "localhost"),
		"-p", strconv.Itoa(argInt(args, "login_port", 6379)),
	}
	if u := argString(args, "login_user", ""); u != "" {
		a = append(a, "--user", u)
	}
	if argBool(args, "tls", tlsDefault) {
		a = append(a, "--tls")
	}
	if ca := argString(args, "ca_certs", ""); ca != "" {
		a = append(a, "--cacert", ca)
	}
	if c := argString(args, "client_cert_file", ""); c != "" {
		a = append(a, "--cert", c)
	}
	if k := argString(args, "client_key_file", ""); k != "" {
		a = append(a, "--key", k)
	}
	if !argBool(args, "validate_certs", true) {
		a = append(a, "--insecure")
	}
	return a
}

// redisCli runs `redis-cli <connArgs> <cmdArgs...>` on the target,
// passing login_password (if set) via the REDISCLI_AUTH environment
// variable rather than a `-a` command-line flag — redis-cli's own
// documented safer alternative, which keeps the password out of the
// target's process listing (`ps`). It is still embedded in the single
// shell command string handed to Connection.Exec, an architectural limit
// of this port: see module.go's own doc comment on how every module
// reaches its target only through Exec's one command string, never a
// dedicated Redis client dialing the target directly the way real
// redis/redis_* modules' Python `redis` client library does.
func redisCli(ctx context.Context, conn remoteexec.Connection, args map[string]any, tlsDefault bool, cmdArgs ...string) (remoteexec.Result, error) {
	all := append(redisConnArgs(args, tlsDefault), cmdArgs...)
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = shellQuote(a)
	}
	cmd := "redis-cli " + strings.Join(quoted, " ")
	if pw := argString(args, "login_password", ""); pw != "" {
		cmd = "REDISCLI_AUTH=" + shellQuote(pw) + " " + cmd
	}
	return conn.Exec(ctx, cmd, nil)
}

// moduleRedis implements Ansible's `redis` (community.general) module: a
// grab-bag of Redis server administration actions — flushing databases,
// getting/setting a single CONFIG parameter, and switching a running
// instance between replica and master mode.
//
// Deviation from real redis: real redis.py (and every other redis_*
// module in this batch) talks to the server through the Python `redis`
// client library over RESP, never a subprocess. This port has no Go
// Redis client wired into remoteexec.Connection, so it substitutes
// shelling out to `redis-cli` on the target instead — same observable
// server-side effect, different transport (see redisCli's own doc
// comment above).
//
// Args: command (required: config|flush|replica|slave — slave is an
// alias for replica, matching real redis); login_host (default
// localhost), login_port (default 6379), login_user, login_password,
// tls (default false), validate_certs (default true), ca_certs,
// client_cert_file, client_key_file — connection options, identical to
// every other redis_* module's own in this batch.
//
// command=config: name (required) and value (required) — reads the
// current value via `CONFIG GET`, and only issues `CONFIG SET` (Changed)
// if it differs; an unknown name at the target fails, matching real
// redis's own `config_get(name)[name]` KeyError -> fail_json.
//
// command=flush: db (int, required if flush_mode=db) and flush_mode
// (default all) — `FLUSHALL` or `redis-cli -n <db> FLUSHDB`. Always
// reports Changed on success, matching real redis's own unconditional
// flush (it never checks whether the database was already empty).
//
// command=replica (slave is an alias): master_host/master_port
// (required when replica_mode=replica, the default) and replica_mode
// (default replica; slave is an alias for replica). Idempotent on
// `INFO replication`'s own role/master_host/master_port fields — a
// no-op if already in the requested mode, otherwise runs `REPLICAOF
// <host> <port>` or `REPLICAOF NO ONE`. Deviation: real redis's
// underlying redis-py client sends the older SLAVEOF command via its own
// slaveof() method; this port uses REPLICAOF, the name Redis itself now
// documents as canonical — both are accepted by every Redis version this
// port targets and have identical effect.
func moduleRedis(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	command, err := requireString(args, "command")
	if err != nil {
		return Result{}, err
	}
	if command == "slave" {
		command = "replica"
	}

	switch command {
	case "config":
		return redisConfigCmd(ctx, conn, args)
	case "flush":
		return redisFlushCmd(ctx, conn, args)
	case "replica":
		return redisReplicaCmd(ctx, conn, args)
	default:
		return Result{}, errArg("redis: command must be one of config, flush, replica, slave, got %q", command)
	}
}

func redisConfigCmd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	value, err := requireString(args, "value")
	if err != nil {
		return Result{}, err
	}
	res, err := redisCli(ctx, conn, args, false, "CONFIG", "GET", name)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("redis: unable to read config %q: %s", name, strings.TrimSpace(res.Stderr))), nil
	}
	lines := strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n")
	if len(lines) < 2 || lines[0] == "" {
		return Fail(fmt.Sprintf("redis: unknown config parameter %q", name)), nil
	}
	old := lines[1]
	if old == value {
		return Ok(fmt.Sprintf("%s already set to %s", name, value)).WithExtra("name", name).WithExtra("value", value), nil
	}
	setRes, err := redisCli(ctx, conn, args, false, "CONFIG", "SET", name, value)
	if err != nil {
		return Result{}, err
	}
	if setRes.RC != 0 {
		return Fail(fmt.Sprintf("redis: unable to write config %q: %s", name, strings.TrimSpace(setRes.Stderr))), nil
	}
	return Changed(fmt.Sprintf("%s set to %s", name, value)).WithExtra("name", name).WithExtra("value", value), nil
}

func redisFlushCmd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	mode := argString(args, "flush_mode", "all")
	switch mode {
	case "all":
		res, err := redisCli(ctx, conn, args, false, "FLUSHALL")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("redis: unable to flush all databases: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("flushed all databases").WithExtra("flushed", true), nil
	case "db":
		if _, ok := args["db"]; !ok {
			return Result{}, errArg("redis: db is required when flush_mode=db")
		}
		db := argInt(args, "db", -1)
		if db < 0 {
			return Result{}, errArg("redis: db must be a non-negative integer")
		}
		res, err := redisCli(ctx, conn, args, false, "-n", strconv.Itoa(db), "FLUSHDB")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(fmt.Sprintf("redis: unable to flush db %d: %s", db, strings.TrimSpace(res.Stderr))), nil
		}
		return Changed(fmt.Sprintf("flushed database %d", db)).WithExtra("flushed", true).WithExtra("db", db), nil
	default:
		return Result{}, errArg("redis: flush_mode must be one of all, db, got %q", mode)
	}
}

func redisReplicaCmd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	mode := argString(args, "replica_mode", "replica")
	if mode == "slave" {
		mode = "replica"
	}
	if mode != "master" && mode != "replica" {
		return Result{}, errArg("redis: replica_mode must be one of master, replica, slave, got %q", mode)
	}

	var masterHost string
	var masterPort int
	if mode == "replica" {
		masterHost = argString(args, "master_host", "")
		if masterHost == "" {
			return Result{}, errArg("redis: master_host is required in replica mode")
		}
		if _, ok := args["master_port"]; !ok {
			return Result{}, errArg("redis: master_port is required in replica mode")
		}
		masterPort = argInt(args, "master_port", 0)
	}

	infoRes, err := redisCli(ctx, conn, args, false, "INFO", "replication")
	if err != nil {
		return Result{}, err
	}
	if infoRes.RC != 0 {
		return Fail("redis: unable to connect to database: " + strings.TrimSpace(infoRes.Stderr)), nil
	}
	role, curHost, curPort := parseRedisReplicationInfo(infoRes.Stdout)

	if mode == "master" && role == "master" {
		return Ok("already master").WithExtra("mode", "master"), nil
	}
	if mode == "replica" && role == "slave" && curHost == masterHost && curPort == masterPort {
		status := map[string]any{"status": "replica", "master_host": masterHost, "master_port": masterPort}
		return Ok("already replicating").WithExtra("mode", status), nil
	}

	var setRes remoteexec.Result
	if mode == "replica" {
		setRes, err = redisCli(ctx, conn, args, false, "REPLICAOF", masterHost, strconv.Itoa(masterPort))
	} else {
		setRes, err = redisCli(ctx, conn, args, false, "REPLICAOF", "NO", "ONE")
	}
	if err != nil {
		return Result{}, err
	}
	if setRes.RC != 0 {
		return Fail("redis: unable to set " + mode + " mode: " + strings.TrimSpace(setRes.Stderr)), nil
	}
	if mode == "replica" {
		status := map[string]any{"status": "replica", "master_host": masterHost, "master_port": masterPort}
		return Changed("set to replica of "+masterHost).WithExtra("mode", status), nil
	}
	return Changed("set to master").WithExtra("mode", "master"), nil
}

// parseRedisReplicationInfo parses `INFO replication`'s own `key:value`
// text protocol for the three fields moduleRedis's idempotency check
// needs.
func parseRedisReplicationInfo(out string) (role, masterHost string, masterPort int) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch k {
		case "role":
			role = v
		case "master_host":
			masterHost = v
		case "master_port":
			masterPort, _ = strconv.Atoi(v)
		}
	}
	return
}
