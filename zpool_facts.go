package modules

import (
	"context"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleZpoolFacts implements Ansible's `zpool_facts` module: a
// read-only counterpart to zpool.go, gathering ZFS pool property facts
// via `zpool get -H -o name,property,value <properties> [<name>]` —
// like zfs_facts.go, this module never writes anything and always
// reports Changed=false, and populates Extra rather than Result.Facts
// (see package_facts.go's own doc comment for the house convention
// this follows).
//
// Args: name (string, optional; aliases pool/zpool) — a specific ZFS
// pool name; when omitted, every imported pool is reported, matching
// real zpool_facts exactly; parsable (bool, default false) — `-p`
// (machine-friendly numeric values); properties (string, default
// "all") — a comma-separated property list, passed straight through to
// `zpool get`.
//
// When name is given, its existence is checked first via zpool.go's
// own zpoolExists helper (`zpool list -H -o name name`, exit 0 iff it
// exists) — reused rather than reimplemented, for the same reason
// zfs_facts.go reuses zfs.go's own zfsDatasetExists; Result{Failed:
// true} if not, matching real zpool_facts' own pool_exists() probe
// and fail_json. On success,
// Extra["zfs_pools"] holds a list of maps, one per matched pool, each
// keyed by property name plus "name" (this port's Extra-based facts
// modules key their list under a name derived from the module, not
// real zpool_facts' own literal "ansible_zfs_pools" — see
// package_facts.go's doc comment for why: Extra populates the module's
// OWN result fields, with ansible_facts merging handled at the
// caller/engine layer, so there is no "ansible_" prefix to preserve
// here). "name" is echoed via Extra only when given (an empty name has
// no single value to echo, matching real zpool_facts' own
// `result["name"] = zpool_facts.name`, which is simply `None` in that
// case and thus not meaningfully different from omitting the key
// here); "parsable" is echoed only when true.
func moduleZpoolFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := ""
	for _, key := range []string{"name", "pool", "zpool"} {
		if s, ok := args[key].(string); ok && s != "" {
			name = s
			break
		}
	}
	parsable := argBool(args, "parsable", false)
	properties := argString(args, "properties", "all")

	if name != "" {
		exists, err := zpoolExists(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail("ZFS pool " + name + " does not exist!"), nil
		}
	}

	cmd := "zpool get -H"
	if parsable {
		cmd += " -p"
	}
	cmd += " -o name,property,value " + shellQuote(properties)
	if name != "" {
		cmd += " " + shellQuote(name)
	}

	out, err := run(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	pools := map[string]map[string]any{}
	var order []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		pool, prop, value := fields[0], fields[1], fields[2]
		if _, ok := pools[pool]; !ok {
			pools[pool] = map[string]any{}
			order = append(order, pool)
		}
		pools[pool][prop] = value
	}
	sort.Strings(order)

	var facts []any
	for _, pool := range order {
		m := pools[pool]
		m["name"] = pool
		facts = append(facts, m)
	}
	if facts == nil {
		facts = []any{}
	}

	res := Ok("").WithExtra("zfs_pools", facts)
	if name != "" {
		res = res.WithExtra("name", name)
	}
	if parsable {
		res = res.WithExtra("parsable", true)
	}
	return res, nil
}
