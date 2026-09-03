package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXattr implements (a subset of) Ansible's `xattr` module:
// manages filesystem extended attributes via `setfattr`/`getfattr`.
//
// Args: path (string, required; aliased from `name`); namespace
// (string, default "user"); key (string) — required for state
// read/present/absent (see below); value (string, optional) — setting
// it implies state=present, matching real xattr's own documented
// behavior exactly; state (read|present|absent|all|keys, default
// "read"); follow (bool, default true) — false passes getfattr/
// setfattr's own `-h` (act on the symlink itself, not its target).
//
// Simplification vs real xattr: this port requires `key` for
// state=read/present/absent (real xattr's doc doesn't spell out what
// state=read with no key does; rather than guess at an undocumented
// fallback, this port fails clearly asking for key instead). state=all
// and state=keys do not need key, matching their own "dump everything"
// nature. No diff_mode support beyond what Result already offers.
func moduleXattr(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := xattrRequirePath(args)
	if err != nil {
		return Result{}, err
	}
	namespace := argString(args, "namespace", "user")
	key := argString(args, "key", "")
	follow := argBool(args, "follow", true)
	flag := ""
	if !follow {
		flag = "-h "
	}

	state := argString(args, "state", "read")
	if _, hasValue := args["value"]; hasValue {
		state = "present"
	}

	switch state {
	case "all":
		out, err := xattrDump(ctx, conn, path, flag)
		if err != nil {
			return Result{}, err
		}
		return Ok(path).WithExtra("xattr", out), nil

	case "keys":
		out, err := xattrDump(ctx, conn, path, flag)
		if err != nil {
			return Result{}, err
		}
		keys := make([]string, 0, len(out))
		for k := range out {
			keys = append(keys, k)
		}
		return Ok(path).WithExtra("xattr", keys), nil

	case "read":
		if key == "" {
			return Result{}, errArg("xattr: key is required when state is read")
		}
		fullKey := namespace + "." + key
		v, ok := xattrGet(ctx, conn, path, fullKey, flag)
		if !ok {
			return Ok(path).WithExtra("xattr", map[string]any{}), nil
		}
		return Ok(path).WithExtra("xattr", map[string]any{fullKey: v}), nil

	case "present":
		if key == "" {
			return Result{}, errArg("xattr: key is required when state is present")
		}
		fullKey := namespace + "." + key
		value := argString(args, "value", "")
		cur, ok := xattrGet(ctx, conn, path, fullKey, flag)
		if ok && cur == value {
			return Ok(fullKey + " already set on " + path), nil
		}
		cmd := "setfattr " + flag + "-n " + shellQuote(fullKey) + " -v " + shellQuote(value) + " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(fullKey + " set on " + path), nil

	case "absent":
		if key == "" {
			return Result{}, errArg("xattr: key is required when state is absent")
		}
		fullKey := namespace + "." + key
		if _, ok := xattrGet(ctx, conn, path, fullKey, flag); !ok {
			return Ok(fullKey + " already absent on " + path), nil
		}
		cmd := "setfattr " + flag + "-x " + shellQuote(fullKey) + " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(fullKey + " removed from " + path), nil

	default:
		return Result{}, errArg("xattr: state must be read, present, absent, all, or keys, got %q", state)
	}
}

func xattrRequirePath(args map[string]any) (string, error) {
	if s, ok := args["path"].(string); ok && s != "" {
		return s, nil
	}
	if s, ok := args["name"].(string); ok && s != "" {
		return s, nil
	}
	return "", errArg("xattr: path (or its alias name) is required")
}

// xattrGet fetches one key's value via getfattr --only-values,
// reporting ok=false if the attribute isn't set.
func xattrGet(ctx context.Context, conn remoteexec.Connection, path, fullKey, flag string) (string, bool) {
	cmd := "getfattr " + flag + "--only-values -n " + shellQuote(fullKey) + " " + shellQuote(path) + " 2>/dev/null"
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil || res.RC != 0 {
		return "", false
	}
	return res.Stdout, true
}

// xattrDump runs `getfattr -d` and parses its "key=\"value\"" lines
// into a map, tolerating an unquoted or missing value.
func xattrDump(ctx context.Context, conn remoteexec.Connection, path, flag string) (map[string]any, error) {
	cmd := "getfattr -d " + flag + shellQuote(path) + " 2>/dev/null"
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if res.RC != 0 {
		return out, nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			out[line] = ""
			continue
		}
		k := line[:idx]
		v := strings.Trim(line[idx+1:], `"`)
		out[k] = v
	}
	return out, nil
}
