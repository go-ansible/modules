package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleZpool implements (a subset of) Ansible's `zpool` module:
// creates, destroys, or reconciles the properties of a ZFS storage
// pool via `zpool create`/`zpool destroy`/`zpool get`/`zpool set`/`zfs
// set` (pool-level vdev topology is only ever set at creation time —
// see below).
//
// Args: name (string, required); state (present|absent, default
// "present"); vdevs (list of {disks: []string, type: stripe|mirror|
// raidz|raidz1|raidz2|raidz3 (default "stripe"), role: log|cache|
// spare|dedup|special}, required to create a new pool) — each vdev
// group becomes one `zpool create` clause: a bare disk list for
// type=stripe, `mirror disk...`/`raidzN disk...` for the redundant
// types, and `log`/`cache`/`spare`/`dedup`/`special` prefixing a
// group given a role; force (bool, default false) — `zpool create -f`;
// altroot (string) — `-R altroot`; mountpoint (string) — `-m
// mountpoint`; temp_name (string) — `-t temp_name`; disable_new_features
// (bool, default false) — `-d`; pool_properties (dict) — `-o
// prop=value` at creation, reconciled via `zpool set` afterward;
// filesystem_properties (dict) — `-O prop=value` at creation (root
// dataset properties), reconciled via `zfs set` afterward — matching
// real zpool's own documented separate pool- vs filesystem-property
// dicts and the two different underlying `get`/`set` tools each one
// uses.
//
// Existence is checked via `zpool list -H -o name name`. Idempotency:
// when the pool already exists, this port does NOT re-verify or alter
// its vdev topology (real zpool itself has no supported way to change
// a pool's own top-level vdev structure short of destroying and
// recreating it, beyond ADDING more vdevs — which this port does not
// implement either, being a real, separate `zpool add` operation this
// port's `vdevs` argument does not distinguish from "vdevs this pool
// was created with"); only pool_properties/filesystem_properties are
// reconciled against an EXISTING pool, each compared via `zpool get
// -H -o value`/`zfs get -H -o value` and only `set` when different —
// the same value-as-trimmed-string comparison caveat documented in
// zfs.go's own doc comment applies here too. state=absent runs `zpool
// destroy name` if it exists (no `-f`, matching there being no
// separate destroy-time force argument documented for real zpool).
func moduleZpool(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("zpool: state must be present or absent, got %q", state)
	}

	exists, err := zpoolExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "zpool destroy "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " destroyed"), nil
	}

	poolProps := zfsPropsArg(map[string]any{"extra_zfs_properties": args["pool_properties"]})
	fsProps := zfsPropsArg(map[string]any{"extra_zfs_properties": args["filesystem_properties"]})
	changed := false

	if !exists {
		vdevs, err := zpoolParseVdevs(args)
		if err != nil {
			return Result{}, err
		}
		if len(vdevs) == 0 {
			return Result{}, errArg("zpool: vdevs is required to create pool %q", name)
		}
		cmd := "zpool create"
		if argBool(args, "force", false) {
			cmd += " -f"
		}
		if v := argString(args, "altroot", ""); v != "" {
			cmd += " -R " + shellQuote(v)
		}
		if v := argString(args, "mountpoint", ""); v != "" {
			cmd += " -m " + shellQuote(v)
		}
		if v := argString(args, "temp_name", ""); v != "" {
			cmd += " -t " + shellQuote(v)
		}
		if argBool(args, "disable_new_features", false) {
			cmd += " -d"
		}
		for _, k := range zfsSortedKeys(poolProps) {
			cmd += " -o " + shellQuote(k+"="+poolProps[k])
		}
		for _, k := range zfsSortedKeys(fsProps) {
			cmd += " -O " + shellQuote(k+"="+fsProps[k])
		}
		cmd += " " + shellQuote(name)
		for _, vd := range vdevs {
			cmd += " " + vd
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(name + " created"), nil
	}

	for _, k := range zfsSortedKeys(poolProps) {
		want := poolProps[k]
		cur, err := zpoolGetProp(ctx, conn, name, k)
		if err != nil {
			return Result{}, err
		}
		if cur == want {
			continue
		}
		if _, err := run(ctx, conn, "zpool set "+shellQuote(k+"="+want)+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		changed = true
	}
	for _, k := range zfsSortedKeys(fsProps) {
		want := fsProps[k]
		cur, err := zfsGetProp(ctx, conn, name, k)
		if err != nil {
			return Result{}, err
		}
		if cur == want {
			continue
		}
		if _, err := run(ctx, conn, "zfs set "+shellQuote(k+"="+want)+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed(name + " updated"), nil
	}
	return Ok(name + " already up to date"), nil
}

func zpoolExists(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "zpool list -H -o name "+shellQuote(name)+" 2>/dev/null")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

func zpoolGetProp(ctx context.Context, conn remoteexec.Connection, name, prop string) (string, error) {
	res, err := runStatus(ctx, conn, "zpool get -H -o value "+shellQuote(prop)+" "+shellQuote(name)+" 2>/dev/null")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	return strings.TrimSpace(res.Stdout), nil
}

// zpoolParseVdevs builds one `zpool create` clause per vdevs entry.
func zpoolParseVdevs(args map[string]any) ([]string, error) {
	raw, ok := args["vdevs"].([]any)
	if !ok {
		return nil, nil
	}
	var clauses []string
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, errArg("zpool: vdevs[%d] must be a mapping", i)
		}
		disks := argStringList(m, "disks")
		if len(disks) == 0 {
			return nil, errArg("zpool: vdevs[%d].disks is required", i)
		}
		typ := argString(m, "type", "stripe")
		role := argString(m, "role", "")
		var parts []string
		if role != "" {
			parts = append(parts, role)
		}
		if typ != "stripe" {
			parts = append(parts, typ)
		}
		parts = append(parts, quotedList(disks)...)
		clauses = append(clauses, strings.Join(parts, " "))
	}
	return clauses, nil
}
