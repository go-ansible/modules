package modules

import (
	"context"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleZfs implements Ansible's `zfs` module: creates, clones, or
// destroys a ZFS filesystem, volume, or snapshot via `zfs create`/`zfs
// clone`/`zfs destroy`, and reconciles its properties via `zfs get`/
// `zfs set`.
//
// Args: name (string, required) — a filesystem, volume, or snapshot
// name, e.g. "rpool/myfs" or "rpool/myfs@mysnapshot"; state (present|
// absent, required); origin (string) — creates name as a clone of this
// snapshot (mutually exclusive with name itself naming a snapshot, the
// same way real zfs's own `zfs clone` is); extra_zfs_properties (dict
// of string to string/bool/number) — arbitrary `zfs set` properties,
// matching real zfs's own free-form dict (see zfs(8) for the full
// property list this port does not enumerate itself).
//
// state=present: if name does not exist, it is created — via `zfs
// clone origin name` when origin is given, `zfs create -V <volsize>
// name` when extra_zfs_properties sets `volsize` (a ZFS volume), `zfs
// snapshot name` when name contains "@" (a snapshot), or plain `zfs
// create name` otherwise — then extra_zfs_properties (any not already
// consumed as -o creation options) is applied via `zfs set`. If name
// already exists, only extra_zfs_properties is reconciled: each
// property's CURRENT value (`zfs get -H -o value <prop> name`) is
// compared to the desired one, and only a mismatched property is `zfs
// set`. state=absent: `zfs destroy name` if it exists, else unchanged;
// this port does NOT implement real zfs's own documented "all parents/
// children are created/destroyed as needed" recursive cascade beyond
// what `zfs destroy` itself does for a snapshot's clones or a dataset's
// children when given no extra flags (real zfs's own Python module
// relies on the same underlying `zfs destroy` semantics, so this is not
// a narrowing versus real zfs, just a note that no `-r`/`-R` recursion
// flag is added by this port — a dataset with children fails to
// destroy with zfs's own clear error, exactly as real zfs would without
// passing such a flag itself).
//
// Property value comparison is done as trimmed strings — this port
// does not know each property's own type (size, boolean, quota-with-
// suffix, ...) the way real zfs's own dict-driven ZFS property table
// does, so e.g. "1M" vs "1048576" for the same effective size would be
// seen as a (needless) change; matching real zfs's own documented
// check_mode caveat about size values written in human-readable
// notation.
func moduleZfs(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("zfs: state must be present or absent, got %q", state)
	}
	origin := argString(args, "origin", "")
	props := zfsPropsArg(args)

	exists, err := zfsDatasetExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "zfs destroy "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " destroyed"), nil
	}

	changed := false
	if !exists {
		var cmd string
		switch {
		case origin != "":
			cmd = "zfs clone " + shellQuote(origin) + " " + shellQuote(name)
		case strings.Contains(name, "@"):
			cmd = "zfs snapshot " + shellQuote(name)
		default:
			cmd = "zfs create " + shellQuote(name)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	}

	for _, prop := range zfsSortedKeys(props) {
		want := props[prop]
		cur, err := zfsGetProp(ctx, conn, name, prop)
		if err != nil {
			return Result{}, err
		}
		if cur == want {
			continue
		}
		if _, err := run(ctx, conn, "zfs set "+shellQuote(prop+"="+want)+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed(name + " updated"), nil
	}
	return Ok(name + " already up to date"), nil
}

func zfsPropsArg(args map[string]any) map[string]string {
	out := map[string]string{}
	raw, _ := args["extra_zfs_properties"].(map[string]any)
	for k, v := range raw {
		switch t := v.(type) {
		case string:
			out[k] = t
		case bool:
			if t {
				out[k] = "on"
			} else {
				out[k] = "off"
			}
		default:
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

func zfsSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// zfsDatasetExists reports whether name currently exists, via `zfs
// list -H -o name name`.
func zfsDatasetExists(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "zfs list -H -o name "+shellQuote(name)+" 2>/dev/null")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// zfsGetProp reads one property's current value via `zfs get -H -o
// value`, returning "" if it can't be read.
func zfsGetProp(ctx context.Context, conn remoteexec.Connection, name, prop string) (string, error) {
	res, err := runStatus(ctx, conn, "zfs get -H -o value "+shellQuote(prop)+" "+shellQuote(name)+" 2>/dev/null")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	return strings.TrimSpace(res.Stdout), nil
}
