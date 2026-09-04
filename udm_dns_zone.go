package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUdmDnsZone implements Ansible's `udm_dns_zone`
// (community.general) module: manages a DNS zone (forward or reverse)
// on a Univention Corporate Server (UCS). See udmBin's own doc comment
// (udm_common.go) for this port's `udm` CLI substitution.
//
// Args: zone (string, required, alias name); type (string, required)
// — forward_zone|reverse_zone, mapped onto the udm module path
// "dns/<type>"; state (present|absent, default "present"); nameserver
// ([]string, default [], required when state=present, matching real
// udm_dns_zone.py's own required_if); interfaces ([]string, default [],
// required when state=present, same required_if) — sent as the zone's
// own "a" attribute (its interface-address records), matching real
// udm_dns_zone.py's own `obj["a"] = interfaces`; refresh/retry/expire/
// ttl (int, defaults 3600/1800/604800/600) — passed straight through as
// plain integer seconds via `--set`. Real udm_dns_zone.py's own
// convert_time() instead reshapes each into a (count, unit) pair (e.g.
// "1 days") before handing it to univention.admin's own Python property
// setter — an artifact of that API's internal representation. The `udm`
// CLI's own refresh/retry/expire/ttl options accept a plain integer
// number of seconds directly (what an operator types at a real `udm`
// prompt), so this port sends that instead of replicating the tuple
// conversion — a deliberate, documented deviation, not an oversight.
// contact (string, default "") — defaults to "root@<zone>." when empty,
// matching real udm_dns_zone.py exactly; mx ([]string, default []).
//
// Zones are top-level UDM objects, created under "cn=dns,<base_dn>" via
// `--position` — unlike udm_dns_record's own subordinate
// `--superordinate` create (see udm_dns_record.go's own doc comment).
func moduleUdmDnsZone(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	zone := argString(args, "zone", argString(args, "name", ""))
	if zone == "" {
		return Result{}, errArg("udm_dns_zone: missing required argument: zone (or its alias name)")
	}
	zoneType, err := requireString(args, "type")
	if err != nil {
		return Result{}, err
	}
	if zoneType != "forward_zone" && zoneType != "reverse_zone" {
		return Result{}, errArg("udm_dns_zone: type must be forward_zone or reverse_zone, got %q", zoneType)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("udm_dns_zone: state must be present or absent, got %q", state)
	}

	baseDN, err := udmBaseDN(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	container := "cn=dns," + baseDN
	modulePath := "dns/" + zoneType
	scope := udmScope{Position: container}

	obj, err := udmFind(ctx, conn, modulePath, "zone="+zone, scope)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if obj == nil {
			return Ok(zone + " already absent"), nil
		}
		if err := udmRemove(ctx, conn, modulePath, obj.DN); err != nil {
			return Result{}, err
		}
		return Changed(zone + " removed"), nil
	}

	nameserver := argStringList(args, "nameserver")
	interfaces := argStringList(args, "interfaces")
	if len(nameserver) == 0 {
		return Result{}, errArg("udm_dns_zone: nameserver is required when state=present")
	}
	if len(interfaces) == 0 {
		return Result{}, errArg("udm_dns_zone: interfaces is required when state=present")
	}

	contact := argString(args, "contact", "")
	if contact == "" {
		contact = "root@" + zone + "."
	}
	desired := map[string][]string{
		"zone":       {zone},
		"nameserver": nameserver,
		"a":          interfaces,
		"refresh":    {strconv.Itoa(argInt(args, "refresh", 3600))},
		"retry":      {strconv.Itoa(argInt(args, "retry", 1800))},
		"expire":     {strconv.Itoa(argInt(args, "expire", 604800))},
		"ttl":        {strconv.Itoa(argInt(args, "ttl", 600))},
		"contact":    {contact},
	}
	if mx := argStringList(args, "mx"); len(mx) > 0 {
		desired["mx"] = mx
	}

	if obj == nil {
		if err := udmCreate(ctx, conn, modulePath, scope, desired); err != nil {
			return Result{}, err
		}
		return Changed(zone + " created"), nil
	}
	changed, err := udmReconcile(ctx, conn, modulePath, obj, desired)
	if err != nil {
		return Result{}, err
	}
	if !changed {
		return Ok(zone + " already up to date"), nil
	}
	return Changed(zone + " updated"), nil
}
