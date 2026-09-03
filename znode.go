package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// zkRun runs one or more ZooKeeper shell commands (newline-separated,
// e.g. "addauth digest user:pass\nget /path") against `zkCli.sh -server
// <hosts>` via stdin — real znode is implemented against kazoo (a
// native ZooKeeper client library); this port has no Go ZooKeeper
// client wired into remoteexec.Connection, so it substitutes shelling
// out to ZooKeeper's own bundled `zkCli.sh` script instead, piping
// commands to it on stdin the same way a human operator would at an
// interactive `zkCli.sh` prompt — the same substitution this project
// already makes for consul_kv.go (`consul` CLI) and lxd_container.go
// (`lxc` CLI).
func zkRun(ctx context.Context, conn remoteexec.Connection, hosts, script string) (remoteexec.Result, error) {
	cmd := "zkCli.sh -server " + shellQuote(hosts)
	return conn.Exec(ctx, cmd, strings.NewReader(script))
}

// zkScript prepends an `addauth <scheme> <credential>` line when
// auth_credential is set, matching real znode's own
// auth_scheme/auth_credential options.
func zkScript(args map[string]any, commands ...string) string {
	var lines []string
	if cred := argString(args, "auth_credential", ""); cred != "" {
		scheme := argString(args, "auth_scheme", "digest")
		lines = append(lines, "addauth "+scheme+" "+cred)
	}
	lines = append(lines, commands...)
	return strings.Join(lines, "\n") + "\n"
}

// moduleZnode implements Ansible's `znode` (community.general) module:
// creates, deletes, or reads a ZooKeeper znode via ZooKeeper's own
// bundled `zkCli.sh` shell client — see zkRun's own doc comment for
// why this port substitutes the CLI for real znode's kazoo client.
//
// Args: hosts (required) — one or more `server:port` entries, joined
// with commas for `zkCli.sh -server`; name (required) — the znode's
// path; auth_scheme (digest|sasl, default "digest"); auth_credential
// — if set, an `addauth <scheme> <credential>` line is sent before
// every other command in the same zkCli session (see zkScript); value
// — the data to set (state=present); state (present|absent, mutually
// exclusive with op); op (get|wait|list, mutually exclusive with
// state); recursive (bool, default false) — state=absent only, uses
// `deleteall` instead of `delete` (real znode uses kazoo's own
// recursive delete; ZooKeeper 3.5+'s `deleteall` zkCli command is the
// closest CLI equivalent — an older ZooKeeper server without
// `deleteall` fails that command, surfaced as Result{Failed:true}
// rather than this port attempting its own recursive `ls`+`delete`
// walk); timeout (default 300) — seconds op=wait polls for.
//
// Deviation from real znode: real znode's own `use_tls` option
// configures kazoo's own TLS transport with a client certificate;
// `zkCli.sh` has no per-invocation TLS flag at all (secure clients
// need a Java `-Dzookeeper.client.secure=true` system property plus a
// separate client configuration/keystore file this port does not
// manage), so use_tls is accepted for compatibility but NOT
// implemented — a znode task against a TLS-only ensemble fails at the
// `zkCli.sh` connection step itself, surfaced as this module's own
// Result{Failed:true} with zkCli's own connection error, not silently
// ignored.
//
// state=present: creates the znode (and any missing ancestor path
// segments, each with empty data — matching real znode's own kazoo
// `ensure_path` behavior) if it doesn't exist, or updates its data via
// `set` if the existing value differs; unchanged if the value already
// matches. state=absent: `delete`/`deleteall`, unchanged if the znode
// doesn't exist. op=get: returns Extra["msg"] (the znode's own data)
// and Extra["stat"] (a map parsed from zkCli's own "key = value" stat
// block, e.g. cZxid/ctime/mZxid/version/...). op=list: returns
// Extra["children"] (a []string parsed from zkCli's own `ls`
// bracketed, comma-separated output). op=wait: polls `stat <path>`
// once a second (bounded by timeout) until the znode exists;
// Result{Failed:true} if it never appears within timeout.
func moduleZnode(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	hostList := argStringList(args, "hosts")
	if len(hostList) == 0 {
		return Result{}, errArg("znode: missing required argument: hosts")
	}
	hosts := strings.Join(hostList, ",")
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	_, hasState := args["state"]
	_, hasOp := args["op"]
	if hasState && hasOp {
		return Result{}, errArg("znode: state and op are mutually exclusive")
	}

	if hasOp {
		op := argString(args, "op", "")
		switch op {
		case "get":
			return znodeGet(ctx, conn, args, hosts, name)
		case "list":
			return znodeList(ctx, conn, args, hosts, name)
		case "wait":
			return znodeWait(ctx, conn, args, hosts, name)
		default:
			return Result{}, errArg("znode: op must be one of get, wait, list, got %q", op)
		}
	}

	state := argString(args, "state", "present")
	switch state {
	case "present":
		return znodeEnsurePresent(ctx, conn, args, hosts, name)
	case "absent":
		return znodeEnsureAbsent(ctx, conn, args, hosts, name)
	default:
		return Result{}, errArg("znode: state must be present or absent, got %q", state)
	}
}

func znodeExists(ctx context.Context, conn remoteexec.Connection, args map[string]any, hosts, path string) (bool, error) {
	res, err := zkRun(ctx, conn, hosts, zkScript(args, "stat "+path))
	if err != nil {
		return false, err
	}
	return res.RC == 0 && !strings.Contains(res.Stdout, "Node does not exist"), nil
}

func znodeEnsurePresent(ctx context.Context, conn remoteexec.Connection, args map[string]any, hosts, path string) (Result, error) {
	value := argString(args, "value", "")

	exists, err := znodeExists(ctx, conn, args, hosts, path)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		// Ensure every ancestor exists first (kazoo's own ensure_path
		// behavior — see moduleZnode's doc comment).
		segments := strings.Split(strings.Trim(path, "/"), "/")
		cur := ""
		for i := 0; i < len(segments)-1; i++ {
			cur += "/" + segments[i]
			ok, err := znodeExists(ctx, conn, args, hosts, cur)
			if err != nil {
				return Result{}, err
			}
			if !ok {
				res, err := zkRun(ctx, conn, hosts, zkScript(args, "create "+cur+" ''"))
				if err != nil {
					return Result{}, err
				}
				if res.RC != 0 {
					return Fail("znode: creating ancestor " + cur + ": " + strings.TrimSpace(res.Stderr+res.Stdout)), nil
				}
			}
		}
		res, err := zkRun(ctx, conn, hosts, zkScript(args, "create "+path+" "+shellQuote(value)))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("znode: creating " + path + ": " + strings.TrimSpace(res.Stderr+res.Stdout)), nil
		}
		return Changed(path + " created"), nil
	}

	current, _, err := znodeReadValue(ctx, conn, args, hosts, path)
	if err != nil {
		return Result{}, err
	}
	if current == value {
		return Ok(path + " already set"), nil
	}
	res, err := zkRun(ctx, conn, hosts, zkScript(args, "set "+path+" "+shellQuote(value)))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("znode: setting " + path + ": " + strings.TrimSpace(res.Stderr+res.Stdout)), nil
	}
	return Changed(path + " updated"), nil
}

func znodeEnsureAbsent(ctx context.Context, conn remoteexec.Connection, args map[string]any, hosts, path string) (Result, error) {
	exists, err := znodeExists(ctx, conn, args, hosts, path)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Ok(path + " already absent"), nil
	}
	verb := "delete"
	if argBool(args, "recursive", false) {
		verb = "deleteall"
	}
	res, err := zkRun(ctx, conn, hosts, zkScript(args, verb+" "+path))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("znode: deleting " + path + ": " + strings.TrimSpace(res.Stderr+res.Stdout)), nil
	}
	return Changed(path + " deleted"), nil
}

// znodeReadValue returns the znode's own data via `get <path>`,
// found=false (not an error) if it doesn't exist.
func znodeReadValue(ctx context.Context, conn remoteexec.Connection, args map[string]any, hosts, path string) (value string, found bool, err error) {
	res, err := zkRun(ctx, conn, hosts, zkScript(args, "get "+path))
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 || strings.Contains(res.Stdout, "Node does not exist") {
		return "", false, nil
	}
	return znodeParseGetValue(res.Stdout), true, nil
}

// znodeParseGetValue extracts the value portion of `get`'s own output
// — everything before the first stat-block line (recognized by its own
// "cZxid = " prefix, the first field zkCli always prints).
func znodeParseGetValue(out string) string {
	idx := strings.Index(out, "cZxid = ")
	if idx < 0 {
		return strings.TrimSpace(out)
	}
	return strings.TrimRight(out[:idx], "\n")
}

// znodeParseStat parses the "key = value" lines of a `get`/`stat`
// stat block into a map.
func znodeParseStat(out string) map[string]any {
	stat := map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, " = ", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if n, err := strconv.Atoi(val); err == nil {
			stat[key] = n
			continue
		}
		stat[key] = val
	}
	return stat
}

func znodeGet(ctx context.Context, conn remoteexec.Connection, args map[string]any, hosts, path string) (Result, error) {
	res, err := zkRun(ctx, conn, hosts, zkScript(args, "get "+path))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 || strings.Contains(res.Stdout, "Node does not exist") {
		return Fail("znode: " + path + " does not exist"), nil
	}
	value := znodeParseGetValue(res.Stdout)
	stat := znodeParseStat(res.Stdout)
	return Ok(value).WithExtra("stat", stat), nil
}

func znodeList(ctx context.Context, conn remoteexec.Connection, args map[string]any, hosts, path string) (Result, error) {
	res, err := zkRun(ctx, conn, hosts, zkScript(args, "ls "+path))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 || strings.Contains(res.Stdout, "Node does not exist") {
		return Fail("znode: " + path + " does not exist"), nil
	}
	children := znodeParseList(res.Stdout)
	return Ok("").WithExtra("children", children), nil
}

// znodeParseList extracts zkCli's own `ls`-output "[a, b, c]" bracketed,
// comma-separated line into a []string (empty for "[]").
func znodeParseList(out string) []string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inner := strings.TrimSpace(line[1 : len(line)-1])
			if inner == "" {
				return []string{}
			}
			var out []string
			for _, part := range strings.Split(inner, ",") {
				out = append(out, strings.TrimSpace(part))
			}
			return out
		}
	}
	return []string{}
}

func znodeWait(ctx context.Context, conn remoteexec.Connection, args map[string]any, hosts, path string) (Result, error) {
	timeoutSec := argInt(args, "timeout", 300)
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for {
		exists, err := znodeExists(ctx, conn, args, hosts, path)
		if err != nil {
			return Result{}, err
		}
		if exists {
			return Ok(path + " appeared"), nil
		}
		if time.Now().After(deadline) {
			return Fail("znode: " + path + " did not appear within " + strconv.Itoa(timeoutSec) + "s"), nil
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
