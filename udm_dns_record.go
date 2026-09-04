package modules

import (
	"context"
	"fmt"
	"net"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUdmDnsRecord implements Ansible's `udm_dns_record`
// (community.general) module: manages a single DNS record inside an
// existing DNS zone on a Univention Corporate Server (UCS). See
// udmBin's own doc comment (udm_common.go) for why this port
// substitutes UCS's own `udm` command-line tool for real
// udm_dns_record's Python-API (univention.admin) implementation.
//
// Args: name (string, required) — the record's own name; for PTR
// records this is the full IP address being pointed FROM, not the
// reverse-zone label, matching real udm_dns_record.py's own naming
// exactly; zone (string, required) — the record's containing DNS zone
// (for PTR records, the full reverse zone, e.g.
// "1.1.192.in-addr.arpa"); type (string, required) —
// host_record|alias|ptr_record|srv_record|txt_record, mapped straight
// onto the udm module path "dns/<type>"; state (present|absent, default
// "present"); data (map, default {}) — passed straight through as
// `--set`/`--remove` flags using ITS OWN key names as the record's UDM
// attribute names (e.g. data.a for a host_record's own A/AAAA
// addresses) — matching real udm_dns_record.py's own `obj.update(data)`,
// which sets whatever keys the caller supplies directly onto the UMC
// object with no schema translation of its own; this port makes the
// identical no-translation choice, so a caller must already know the
// UDM property name for whatever record type they're managing (as real
// udm_dns_record's own documentation examples do: data.a for
// host_record, data.ptr_record for ptr_record). Every string value
// anywhere in data (scalar or list element) that parses as an IPv6
// address is rewritten to its fully-expanded colon-hex form before
// being sent, matching real udm_dns_record.py's own _normalize_data_ips
// (keeps IPv6 AAAA/PTR data comparable across re-runs regardless of how
// the caller abbreviated it).
//
// DNS records are subordinate UDM objects: created inside an existing
// dns/forward_zone or dns/reverse_zone via `--superordinate`, not
// `--position` — matching real udm_dns_record.py's own
// `umc_module_for_add(..., superordinate=so[0])` call, the one place in
// this module family that isn't a plain top-level `--position` create
// (see udm_dns_zone.go). Unlike real udm_dns_record.py, this port does
// NOT first verify the zone itself exists via a separate
// forward_zone.lookup/reverse_zone.lookup call — a missing zone simply
// surfaces as whatever error `udm ... create --superordinate
// <missing-zone-dn>` itself reports, rather than this port's own
// friendlier "did not find zone" message.
//
// For type=ptr_record, name must be a literal IP address: this port
// computes its DNS reverse-pointer name exactly like Python's
// ipaddress.ip_address(name).reverse_pointer (see udmReversePointer)
// and strips the zone's own suffix from it to get the record's
// relativeDomainName within that zone, matching real udm_dns_record.py's
// own workname computation exactly. obj["ip"]/obj["address"] are set
// instead of obj["name"] for this type, again matching the real module.
func moduleUdmDnsRecord(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	zone, err := requireString(args, "zone")
	if err != nil {
		return Result{}, err
	}
	recType, err := requireString(args, "type")
	if err != nil {
		return Result{}, err
	}
	switch recType {
	case "host_record", "alias", "ptr_record", "srv_record", "txt_record":
	default:
		return Result{}, errArg("udm_dns_record: type must be one of host_record, alias, ptr_record, srv_record, txt_record, got %q", recType)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("udm_dns_record: state must be present or absent, got %q", state)
	}

	workname := name
	if recType == "ptr_record" {
		if !strings.Contains(zone, "arpa") {
			return Fail(fmt.Sprintf("handling PTR record for %s in zone %s failed: zone must be reversed zone for ptr_record. (e.g. 1.1.192.in-addr.arpa)", name, zone)), nil
		}
		revPtr, err := udmReversePointer(name)
		if err != nil {
			return Fail(fmt.Sprintf("handling PTR record for %s in zone %s failed: %v", name, zone, err)), nil
		}
		idx := strings.Index(revPtr, zone)
		if idx < 0 {
			return Fail(fmt.Sprintf("handling PTR record for %s in zone %s failed: reversed IP address %s is not part of zone.", name, zone, revPtr)), nil
		}
		if idx > 0 {
			workname = revPtr[:idx-1]
		} else {
			workname = ""
		}
	}

	baseDN, err := udmBaseDN(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	zoneDN := "zoneName=" + zone + ",cn=dns," + baseDN
	modulePath := "dns/" + recType
	scope := udmScope{Superordinate: zoneDN}

	obj, err := udmFind(ctx, conn, modulePath, "name="+workname, scope)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if obj == nil {
			return Ok(name + " already absent"), nil
		}
		if err := udmRemove(ctx, conn, modulePath, obj.DN); err != nil {
			return Result{}, err
		}
		return Changed(name+" removed").WithExtra("container", zoneDN), nil
	}

	dataMap, _ := args["data"].(map[string]any)
	desired := udmNormalizeDnsData(dataMap)
	if recType == "ptr_record" {
		desired["ip"] = []string{name}
		desired["address"] = []string{workname}
	} else {
		desired["name"] = []string{name}
	}

	if obj == nil {
		if err := udmCreate(ctx, conn, modulePath, scope, desired); err != nil {
			return Result{}, err
		}
		return Changed(name+" created").WithExtra("container", zoneDN), nil
	}
	changed, err := udmReconcile(ctx, conn, modulePath, obj, desired)
	if err != nil {
		return Result{}, err
	}
	if !changed {
		return Ok(name+" already up to date").WithExtra("container", zoneDN), nil
	}
	return Changed(name+" updated").WithExtra("container", zoneDN), nil
}

// udmNormalizeDnsData converts a `data` module argument into
// udm_common.go's map[string][]string --set/--remove shape, expanding
// any IPv6 address value (scalar or list element) to its fully-exploded
// colon-hex form — see moduleUdmDnsRecord's own doc comment.
func udmNormalizeDnsData(data map[string]any) map[string][]string {
	out := make(map[string][]string, len(data))
	for k, v := range data {
		switch val := v.(type) {
		case []any:
			vals := make([]string, 0, len(val))
			for _, item := range val {
				vals = append(vals, udmNormalizeIP(fmt.Sprint(item)))
			}
			out[k] = vals
		default:
			out[k] = []string{udmNormalizeIP(fmt.Sprint(v))}
		}
	}
	return out
}

// udmNormalizeIP rewrites s to its fully-expanded colon-hex form if it
// parses as an IPv6 address; every other string (including IPv4
// addresses, which real _normalize_data_ips also leaves untouched) is
// returned unchanged.
func udmNormalizeIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil || !strings.Contains(s, ":") {
		return s
	}
	v6 := ip.To16()
	if v6 == nil {
		return s
	}
	parts := make([]string, 8)
	for i := 0; i < 8; i++ {
		parts[i] = fmt.Sprintf("%02x%02x", v6[i*2], v6[i*2+1])
	}
	return strings.Join(parts, ":")
}
