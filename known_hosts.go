package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKnownHosts implements (a subset of) Ansible's `known_hosts`
// module: adds or removes an SSH host key line in a known_hosts-style
// file.
//
// Args: name (string, required) — the host (aliased from `host` in
// real Ansible; this port only accepts `name`, since args here are
// already resolved by the caller before reaching a module — see
// module.go's doc comment); key (string, required when state=present)
// — the full known_hosts line content (hostname/pattern plus key type
// plus key data, exactly as it would appear in the file); path
// (string, default "~/.ssh/known_hosts"); state (present|absent,
// default "present").
//
// Simplifications vs real known_hosts: no hash_host (real known_hosts
// can store a hashed hostname instead of plaintext via `ssh-keygen
// -H`; this port always writes/matches the plaintext name); state=
// absent removes every line for `name` that this port can find via a
// plain substring grep for name (not `ssh-keygen -R`'s own matching
// logic, which understands hashed entries and comma-separated
// hostname/IP pairs) — a real known_hosts entry using a hashed
// hostname will not be found or removed by this port. Idempotency for
// state=present is an exact full-line match (grep -qxF): a key
// re-formatted or re-ordered but semantically identical to an existing
// entry is NOT recognized as already present, and will be appended
// as a (functionally redundant, but textually different) duplicate
// line — real known_hosts avoids this by parsing and comparing the
// key material itself.
func moduleKnownHosts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	path := argString(args, "path", "~/.ssh/known_hosts")
	state := argString(args, "state", "present")

	// Real's known_hosts ENDS with
	//
	//	results = copy.copy(module.params)
	//	results.update(enforce_state(module, module.params))
	//	module.exit_json(**results)
	//
	// so every parameter comes back as a result key -- name, key, path,
	// hash_host and state -- carrying a "# TODO: deprecate returning
	// everything that was passed in" in real's own source. Undesirable
	// upstream or not, it is the behaviour, and this module returned
	// changed/failed/msg with an English sentence instead.
	//
	// `path` in Extra is also what makes addPathInfo (Registry.Run)
	// fill in uid/gid/owner/group/mode/size, since that is the key it
	// looks for. It OVERWRITES state with the file's own -- measured:
	// real reports state=file here, not the parameter's "present".
	echo := func(r Result) Result {
		r.NoMsg = true
		r = r.WithExtra("name", name).
			WithExtra("path", path).
			WithExtra("state", state).
			WithExtra("hash_host", argBool(args, "hash_host", false))
		if k, ok := args["key"].(string); ok {
			r = r.WithExtra("key", k)
		}
		return r
	}

	switch state {
	case "present":
		key, err := requireString(args, "key")
		if err != nil {
			return Result{}, errArg("known_hosts: key is required when state is present")
		}
		present, err := knownHostLinePresent(ctx, conn, path, key)
		if err != nil {
			return Result{}, err
		}
		if present {
			return echo(Ok("")), nil
		}
		dir := shellDirname(path)
		cmd := "mkdir -p " + shellQuote(dir) + " && printf '%s\\n' " + shellQuote(key) + " >> " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return echo(Changed("")), nil

	case "absent":
		present, err := knownHostNamePresent(ctx, conn, path, name)
		if err != nil {
			return Result{}, err
		}
		if !present {
			return echo(Ok("")), nil
		}
		cmd := "grep -v " + shellQuote(name) + " " + shellQuote(path) + " > " + shellQuote(path+".tmp") +
			" && mv " + shellQuote(path+".tmp") + " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return echo(Changed("")), nil

	default:
		return Result{}, errArg("known_hosts: state must be present or absent, got %q", state)
	}
}

// knownHostLinePresent reports whether path already contains key as an
// exact line (see moduleKnownHosts's doc comment on why this is a
// stricter match than real known_hosts' key-aware comparison). A
// nonexistent path is treated as "not present", not an error.
func knownHostLinePresent(ctx context.Context, conn remoteexec.Connection, path, key string) (bool, error) {
	res, err := runStatus(ctx, conn, "grep -qxF "+shellQuote(key)+" "+shellQuote(path)+" 2>/dev/null")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// knownHostNamePresent reports whether any line in path mentions name
// (a plain substring grep — see moduleKnownHosts's doc comment on its
// limitations versus ssh-keygen -R's hashed-hostname-aware matching).
func knownHostNamePresent(ctx context.Context, conn remoteexec.Connection, path, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "grep -q "+shellQuote(name)+" "+shellQuote(path)+" 2>/dev/null")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// shellDirname returns path's parent directory using only string
// manipulation (no target round trip needed) — good enough for the
// paths known_hosts deals with (always an absolute or ~-relative file
// path with at least one '/').
func shellDirname(path string) string {
	i := lastIndexByte(path, '/')
	if i <= 0 {
		return "."
	}
	return path[:i]
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
