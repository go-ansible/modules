package modules

import (
	"context"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleZfsFacts implements Ansible's `zfs_facts` module: a read-only
// counterpart to zfs.go, gathering ZFS dataset property facts via `zfs
// get -H -o name,property,value <properties> <name>` — like
// package_facts.go/service_facts.go/usb_facts.go, this module never
// writes anything and always reports Changed=false (see those files'
// own doc comments for why this port's facts modules populate Extra
// rather than the Result.Facts field).
//
// Args: name (string, required; aliases ds/dataset) — a ZFS dataset
// name, e.g. "rpool/myfs"; recurse (bool, default false) — `-r`;
// parsable (bool, default false) — `-p` (machine-friendly numeric
// property values rather than "43.8G"-style human units); properties
// (string, default "all") — a comma-separated property list, passed
// straight through to `zfs get`, matching real zfs_facts exactly;
// type ([]string, default ["all"]) — which dataset types to include
// (filesystem|volume|snapshot|bookmark|all); "all" is mutually
// exclusive with any other value, matching real zfs_facts' own
// validation; depth (int, default 0) — recursion depth limit (`-d
// depth`; 0 means the flag is omitted, matching real zfs_facts exactly
// — real zfs itself then defaults to unlimited depth).
//
// name's existence is checked first via zfs.go's own zfsDatasetExists
// helper (`zfs list -H -o name name`, exit 0 iff it exists) — reused
// rather than reimplemented, since zfs.go already has to solve the
// exact same "does this dataset exist" question for its own
// state=present/absent logic; Result{Failed:true} if not, matching
// real zfs_facts' own dataset_exists() probe and fail_json. On success,
// Extra["zfs_datasets"] holds a list of maps, one per matched dataset,
// each keyed by property name plus "name" — matching real zfs_facts'
// own ansible_facts.zfs_datasets shape exactly (each `zfs get` output
// line is "dataset\tproperty\tvalue", grouped by dataset). "name" is
// always echoed via Extra; "parsable"/"recurse" are only echoed when
// true, matching real zfs_facts' own conditional result fields.
func moduleZfsFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := zfsFactsName(args)
	if name == "" {
		return Result{}, errArg("zfs_facts: missing required argument: name (or its aliases ds/dataset)")
	}
	recurse := argBool(args, "recurse", false)
	parsable := argBool(args, "parsable", false)
	properties := argString(args, "properties", "all")
	depth := argInt(args, "depth", 0)

	types := argStringList(args, "type")
	if len(types) == 0 {
		types = []string{"all"}
	}
	hasAll, multi := false, len(types) > 1
	for _, t := range types {
		switch t {
		case "all", "filesystem", "volume", "snapshot", "bookmark":
		default:
			return Result{}, errArg("zfs_facts: type must be one of all, filesystem, volume, snapshot, bookmark, got %q", t)
		}
		if t == "all" {
			hasAll = true
		}
	}
	if hasAll && multi {
		return Result{}, errArg("zfs_facts: value 'all' for type is mutually exclusive with other values")
	}

	exists, err := zfsDatasetExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("ZFS dataset " + name + " does not exist!"), nil
	}

	cmd := "zfs get -H"
	if parsable {
		cmd += " -p"
	}
	if recurse {
		cmd += " -r"
	}
	if depth != 0 {
		cmd += " -d " + strconv.Itoa(depth)
	}
	cmd += " -t " + strings.Join(types, ",")
	cmd += " -o name,property,value " + shellQuote(properties) + " " + shellQuote(name)

	out, err := run(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	datasets := map[string]map[string]any{}
	var order []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		ds, prop, value := fields[0], fields[1], fields[2]
		if _, ok := datasets[ds]; !ok {
			datasets[ds] = map[string]any{}
			order = append(order, ds)
		}
		datasets[ds][prop] = value
	}
	sort.Strings(order)

	var facts []any
	for _, ds := range order {
		m := datasets[ds]
		m["name"] = ds
		facts = append(facts, m)
	}
	if facts == nil {
		facts = []any{}
	}

	res := Ok("").WithExtra("name", name).WithExtra("zfs_datasets", facts)
	if parsable {
		res = res.WithExtra("parsable", true)
	}
	if recurse {
		res = res.WithExtra("recurse", true)
	}
	return res, nil
}

func zfsFactsName(args map[string]any) string {
	for _, key := range []string{"name", "ds", "dataset"} {
		if s, ok := args[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
