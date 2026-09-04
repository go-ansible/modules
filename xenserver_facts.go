package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// xenserverCodenames maps a XenServer product version to its release
// codename, matching real XenServerFacts.codes exactly.
var xenserverCodenames = map[string]string{
	"5.5.0":   "george",
	"5.6.100": "oxford",
	"6.0.0":   "boston",
	"6.1.0":   "tampa",
	"6.2.0":   "clearwater",
}

// moduleXenserverFacts implements Ansible's `xenserver_facts`
// (community.general) module: a read-only fact-gathering module for a
// XenServer host/pool. See xeBin's own doc comment (xenserver_common.go)
// for this port's `xe` CLI substitution.
//
// Takes no arguments (matching real xenserver_facts.py's own empty
// `options: {}`) and, like real xenserver_facts.py itself, connects to
// the LOCAL XAPI socket only — it has no hostname/username/password
// arguments, unlike its xenserver_guest* siblings, so it must run ON
// the XenServer host's own dom0 (typically without delegate_to, unlike
// the guest-management modules' own `delegate_to: localhost` examples).
//
// Returns ansible_facts with: xenserver_version/xenserver_codename —
// this port reads the pool's first host's own real product_version (via
// `xe host-param-list`'s own software-version compound field) and maps
// it through xenserverCodenames, DELIBERATELY DEVIATING from real
// xenserver_facts.py here: the real module actually queries the Python
// `distro` package's own Linux-distribution detection on whatever
// machine ansible-core itself is running on — not the XenServer host at
// all — which only produces a meaningful xenserver_version when the
// module happens to run ON dom0 itself (a known quirk of the real
// module, not a design this port has reason to reproduce faithfully);
// xs_networks/xs_pifs/xs_vlans (keyed by name_label/interface-name/tag
// respectively, matching real change_keys()); xs_vms/xs_srs (keyed by
// name_label) — each holding this port's own `xe *-param-list` output
// per object (field names in `xe`'s own hyphenated form, e.g.
// "power-state", not XAPI's raw underscored "power_state" real
// xenserver_facts.py returns — a necessary consequence of going through
// the CLI instead of XenAPI's own record dicts). Each of these five
// keys is omitted entirely when empty, matching real xenserver_facts.py
// exactly. A "ref" field is added to every record with the object's own
// UUID (this port's closest equivalent to XenAPI's own opaque
// reference, which `xe` never exposes).
func moduleXenserverFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	facts := map[string]any{}

	version, codename := xeHostVersion(ctx, conn, args)
	facts["xenserver_version"] = version
	if codename != "" {
		facts["xenserver_codename"] = codename
	}

	if vlans, err := xeListRecordsBy(ctx, conn, args, "vlan", "tag"); err == nil && len(vlans) > 0 {
		facts["xs_vlans"] = vlans
	}
	if pifs, err := xeListPIFs(ctx, conn, args); err == nil && len(pifs) > 0 {
		facts["xs_pifs"] = pifs
	}
	if networks, err := xeListRecordsBy(ctx, conn, args, "network", "name-label"); err == nil && len(networks) > 0 {
		facts["xs_networks"] = networks
	}
	if vms, err := xeListRecordsBy(ctx, conn, args, "vm", "name-label"); err == nil && len(vms) > 0 {
		facts["xs_vms"] = vms
	}
	if srs, err := xeListRecordsBy(ctx, conn, args, "sr", "name-label"); err == nil && len(srs) > 0 {
		facts["xs_srs"] = srs
	}

	res := Ok("gathered XenServer facts")
	res.Facts = facts
	return res, nil
}

func xeHostVersion(ctx context.Context, conn remoteexec.Connection, args map[string]any) (string, string) {
	out, err := run(ctx, conn, xeCmdLine(args, []string{"host-list", "params=uuid", "--minimal"}))
	if err != nil {
		return "", ""
	}
	uuids := xeParseList(out)
	if len(uuids) == 0 {
		return "", ""
	}
	hostParams, err := xeParamList(ctx, conn, args, "host", uuids[0])
	if err != nil {
		return "", ""
	}
	sw := xeParseCompound(hostParams["software-version"])
	version := sw["product_version"]
	return version, xenserverCodenames[version]
}

// xeListRecordsBy lists every object of objType and returns them keyed
// by their own keyField value, each holding its own `xe
// <objType>-param-list` field map plus a "ref" entry (its UUID) —
// see moduleXenserverFacts's own doc comment.
func xeListRecordsBy(ctx context.Context, conn remoteexec.Connection, args map[string]any, objType, keyField string) (map[string]any, error) {
	out, err := run(ctx, conn, xeCmdLine(args, []string{objType + "-list", "params=uuid", "--minimal"}))
	if err != nil {
		return nil, err
	}
	out2 := strings.TrimSpace(out)
	if out2 == "" {
		return nil, nil
	}
	result := map[string]any{}
	for _, uuid := range xeParseList(out2) {
		params, err := xeParamList(ctx, conn, args, objType, uuid)
		if err != nil {
			continue
		}
		key := params[keyField]
		if key == "" {
			key = uuid
		}
		rec := stringMapToAny(params)
		rec["ref"] = uuid
		result[key] = rec
	}
	return result, nil
}

// xeListPIFs mirrors real get_pifs(): for every PIF, keys it as
// "eth<N>" or "bond<N>" (N in 0..6) when its own "device" field
// matches, matching real get_pifs()'s own identical narrowing (a PIF
// on any other device naming scheme, e.g. a modern "enX" NIC name, is
// silently skipped — not a gap this port introduces on its own).
func xeListPIFs(ctx context.Context, conn remoteexec.Connection, args map[string]any) (map[string]any, error) {
	out, err := run(ctx, conn, xeCmdLine(args, []string{"pif-list", "params=uuid", "--minimal"}))
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	for _, uuid := range xeParseList(out) {
		params, err := xeParamList(ctx, conn, args, "pif", uuid)
		if err != nil {
			continue
		}
		device := params["device"]
		for i := 0; i <= 6; i++ {
			eth := "eth" + strconv.Itoa(i)
			bond := "bond" + strconv.Itoa(i)
			if device == eth || device == bond {
				rec := stringMapToAny(params)
				rec["ref"] = uuid
				result[device] = rec
			}
		}
	}
	return result, nil
}
