package modules

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// masInstalledRE matches one line of `mas list`'s own output:
// "<id> <Name> (<version>)".
var masInstalledRE = regexp.MustCompile(`^(\d+)\s`)

// masOutdatedRE matches one line of `mas outdated`'s own output:
// "<id> <Name> (<installed> -> <available>)".
var masOutdatedRE = regexp.MustCompile(`^(\d+)\s`)

// masInstalledIDs runs `mas list` and returns the set of installed app
// IDs.
func masInstalledIDs(ctx context.Context, conn remoteexec.Connection) (map[int]bool, error) {
	res, err := runStatus(ctx, conn, "mas list 2>/dev/null")
	if err != nil {
		return nil, err
	}
	ids := map[int]bool{}
	if res.RC != 0 {
		return ids, nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		m := masInstalledRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		ids[id] = true
	}
	return ids, nil
}

// masOutdatedIDs runs `mas outdated` and returns the set of app IDs
// with an update available.
func masOutdatedIDs(ctx context.Context, conn remoteexec.Connection) (map[int]bool, error) {
	res, err := runStatus(ctx, conn, "mas outdated 2>/dev/null")
	if err != nil {
		return nil, err
	}
	ids := map[int]bool{}
	if res.RC != 0 {
		return ids, nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		m := masOutdatedRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		ids[id] = true
	}
	return ids, nil
}

// moduleMas implements Ansible's `mas` (community.general) module:
// installs, uninstalls, or updates macOS applications from the Mac App
// Store, via the `mas` CLI — real mas already shells out to the `mas`
// binary itself (there is no library form to substitute; the Mac App
// Store has no public API `mas` itself doesn't already wrap this same
// way), so this port's architecture matches real mas's own here.
//
// macOS only, and only meaningful when the Apple ID using `mas` is
// already signed in to the App Store (matching real mas's own
// documented requirement — this port does not attempt to check or
// drive that sign-in state, it is out of scope for any CLI wrapper).
// This port gates on `command -v mas` (matching htpasswd.go's own
// "hard-require the binary, fail cleanly rather than fake it"
// convention) rather than also checking `uname` for Darwin — a
// missing `mas` binary already fails cleanly and correctly on every
// non-macOS target too, since `mas` itself does not exist there.
//
// Args: id ([]int) — Mac App Store numeric identifiers (from `mas
// search APP_NAME`); state (present|absent|latest, default "present");
// upgrade_all (bool, default false, aliased "upgrade") — runs `mas
// upgrade` with no arguments, updating every installed app regardless
// of `id`.
//
// state=present: installs any id in `id` not already listed in `mas
// list`'s own output; already-installed ids are left at whatever
// version they're at (matching real mas's own documented "present"
// semantics — it does not imply "latest"). state=latest: installs any
// missing id (a fresh install is already the latest release), and for
// an id already installed, runs `mas upgrade <id>` only if that id
// appears in `mas outdated`'s own output. state=absent: `mas uninstall
// <id>` for any id currently installed (matching real mas's own
// documented note that this needs root — a permission failure here
// surfaces as Result{Failed:true} with `mas`'s own stderr, not a Go
// error, since the request itself was well-formed). upgrade_all=true
// additionally (or exclusively, if `id` is empty) runs `mas upgrade`
// with no id arguments, Changed=true only if `mas outdated` reported
// at least one app before running it.
func moduleMas(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := run(ctx, conn, "command -v mas"); err != nil {
		return Fail("mas: the mas-cli binary is required on the target and was not found in PATH " +
			"(this port shells out to the real mas tool rather than reimplementing Mac App Store access — " +
			"see moduleMas's doc comment; mas is macOS-only)"), nil
	}

	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "latest" {
		return Result{}, errArg("mas: state must be one of present, absent, latest, got %q", state)
	}
	ids := argIntList(args, "id")
	upgradeAll := argBool(args, "upgrade_all", argBool(args, "upgrade", false))

	var actions []string

	if upgradeAll {
		outdated, err := masOutdatedIDs(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if len(outdated) > 0 {
			res, err := conn.Exec(ctx, "mas upgrade", nil)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("mas: upgrading all apps: " + strings.TrimSpace(res.Stderr)), nil
			}
			actions = append(actions, "upgrade_all")
		}
	}

	if len(ids) > 0 {
		installed, err := masInstalledIDs(ctx, conn)
		if err != nil {
			return Result{}, err
		}

		switch state {
		case "absent":
			for _, id := range ids {
				if !installed[id] {
					continue
				}
				res, err := conn.Exec(ctx, "mas uninstall "+strconv.Itoa(id), nil)
				if err != nil {
					return Result{}, err
				}
				if res.RC != 0 {
					return Fail("mas: uninstalling " + strconv.Itoa(id) + ": " + strings.TrimSpace(res.Stderr)), nil
				}
				actions = append(actions, "uninstall:"+strconv.Itoa(id))
			}
		case "latest":
			outdated, err := masOutdatedIDs(ctx, conn)
			if err != nil {
				return Result{}, err
			}
			for _, id := range ids {
				sid := strconv.Itoa(id)
				if !installed[id] {
					res, err := conn.Exec(ctx, "mas install "+sid, nil)
					if err != nil {
						return Result{}, err
					}
					if res.RC != 0 {
						return Fail("mas: installing " + sid + ": " + strings.TrimSpace(res.Stderr)), nil
					}
					actions = append(actions, "install:"+sid)
					continue
				}
				if outdated[id] {
					res, err := conn.Exec(ctx, "mas upgrade "+sid, nil)
					if err != nil {
						return Result{}, err
					}
					if res.RC != 0 {
						return Fail("mas: upgrading " + sid + ": " + strings.TrimSpace(res.Stderr)), nil
					}
					actions = append(actions, "upgrade:"+sid)
				}
			}
		default: // present
			for _, id := range ids {
				if installed[id] {
					continue
				}
				sid := strconv.Itoa(id)
				res, err := conn.Exec(ctx, "mas install "+sid, nil)
				if err != nil {
					return Result{}, err
				}
				if res.RC != 0 {
					return Fail("mas: installing " + sid + ": " + strings.TrimSpace(res.Stderr)), nil
				}
				actions = append(actions, "install:"+sid)
			}
		}
	}

	if len(actions) == 0 {
		return Ok("unchanged"), nil
	}
	return Changed(strings.Join(actions, ", ")), nil
}

// argIntList reads a module argument as a []int, accepting a single
// number or a list.
func argIntList(args map[string]any, key string) []int {
	v, ok := args[key]
	if !ok {
		return nil
	}
	toInt := func(x any) (int, bool) {
		switch n := x.(type) {
		case int:
			return n, true
		case int64:
			return int(n), true
		case float64:
			return int(n), true
		case string:
			i, err := strconv.Atoi(n)
			return i, err == nil
		}
		return 0, false
	}
	switch list := v.(type) {
	case []any:
		out := make([]int, 0, len(list))
		for _, item := range list {
			if n, ok := toInt(item); ok {
				out = append(out, n)
			}
		}
		return out
	default:
		if n, ok := toInt(v); ok {
			return []int{n}
		}
	}
	return nil
}
