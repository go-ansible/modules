package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOneVnet implements Ansible's `one_vnet` module: creates,
// updates, or deletes an OpenNebula virtual network, via the `onevnet`
// CLI (see one_common.go's own doc comment).
//
// Args: id (int) / name (string) — mutually exclusive, exactly one
// required; state (present|absent, default "present"); template
// (string, raw OpenNebula network template text — required when
// state=present).
//
// Structurally identical to one_template.go's own present/absent
// create/update/delete flow (same real one_vnet.py shape: create via
// `self.one.vn.allocate`, update via `self.one.vn.update(id, data, 0)`
// — a full REPLACE, matching `onevnet update <id> -` with no
// `--append`), so see one_template.go's own doc comment for the shared
// "always writes on update, Changed computed from a before/after
// TEMPLATE-map diff" behavior this port reproduces identically here.
//
// Extra fields (state=present only) mirror real one_vnet's own
// get_template_info(): id, name, template, user_name, user_id,
// group_name, group_id, permissions, clusters, bridge, bride_type (sic
// — real one_vnet.py's own get_template_info dict literally has the
// key "bride_type", NOT "bridge_type" as its own RETURN documentation
// block promises: a genuine upstream doc/code typo this port matches
// from the actual code, the same "read source over docs" stance
// documented in one_image.go's own user_id/owner_id note), unless
// there also exists a project-wide reason to prefer the documented
// name — there is not, so this port emits the CODE's own key), unless
// found otherwise), parent_network_id, vn_mad, phydev, vlan_id,
// outer_vlan_id, used_leases, vrouters, ar_pool (each address-range
// entry's own ar_id/mac/size/type/allocated/ip/global_prefix/
// parent_network_ar_id/ula_prefix/vn_mad).
func moduleOneVnet(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	_, hasID := args["id"]
	_, hasName := args["name"]
	if hasID && hasName {
		return Result{}, errArg("one_vnet: id and name are mutually exclusive")
	}
	if !hasID && !hasName {
		return Result{}, errArg("one_vnet: one of id or name is required")
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent":
	default:
		return Result{}, errArg("one_vnet: state must be one of present, absent, got %q", state)
	}
	templateData := argString(args, "template", "")
	if state == "present" && templateData == "" {
		return Result{}, errArg("one_vnet: template is required when state is present")
	}

	url := oneAuth(args)
	if res, ok := oneRequireBinary(ctx, conn, "onevnet", "one_vnet"); !ok {
		return res, nil
	}

	vnet, found, err := oneVnetResolve(ctx, conn, url, args)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !found {
			return Ok(""), nil
		}
		res, err := oneRun(ctx, conn, url, "onevnet", "delete", vnet.childText("ID"))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_vnet: deleting network: " + oneErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	if !found {
		if hasID {
			return Fail("one_vnet: There is no template with id=" + argString(args, "id", "")), nil
		}
		name := argString(args, "name", "")
		body := "NAME = \"" + name + "\"\n" + templateData
		res, err := oneRunStdin(ctx, conn, url, "onevnet", body, "create", "-")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_vnet: creating network: " + oneErrMsg(res)), nil
		}
		pool, err := oneListXML(ctx, conn, url, "onevnet")
		if err != nil {
			return Result{}, err
		}
		created, ok := oneResolveByName(pool, "VNET", name)
		if !ok {
			return Fail("one_vnet: network was created but could not be found afterwards"), nil
		}
		out := Changed("")
		return oneVnetResultWithFacts(out, created), nil
	}

	beforeMap := map[string]any{}
	if before, ok := vnet.child("TEMPLATE"); ok {
		beforeMap = before.toMap()
	}
	res, err := oneRunStdin(ctx, conn, url, "onevnet", templateData, "update", vnet.childText("ID"), "-")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("one_vnet: updating network: " + oneErrMsg(res)), nil
	}
	updated, _, err := oneShowXML(ctx, conn, url, "onevnet", vnet.childText("ID"))
	if err != nil {
		return Result{}, err
	}
	afterMap := map[string]any{}
	if after, ok := updated.child("TEMPLATE"); ok {
		afterMap = after.toMap()
	}
	out := Result{Changed: !oneMapsEqual(beforeMap, afterMap)}
	return oneVnetResultWithFacts(out, updated), nil
}

// oneVnetResolve resolves id/name against `onevnet list -x`'s
// VNET_POOL/VNET. Like one_template.go's own oneVMTemplateResolve, id=0
// is treated as "no id given" (falls through to name resolution),
// faithfully matching real one_vnet.py's own `if requested_id:` falsy-
// for-zero bug — unlike one_image.py, which explicitly special-cases
// id=0 (its own comment: "Using 'if id:' doesn't work properly when
// id=0"), one_vnet.py/one_template.py never got that same fix, verified
// directly against both real sources.
func oneVnetResolve(ctx context.Context, conn remoteexec.Connection, url string, args map[string]any) (oneXMLNode, bool, error) {
	if v, ok := args["id"]; ok && v != nil && fmtAny(v) != "0" {
		return oneShowXML(ctx, conn, url, "onevnet", fmtAny(v))
	}
	name := argString(args, "name", "")
	if name == "" {
		return oneXMLNode{}, false, nil
	}
	pool, err := oneListXML(ctx, conn, url, "onevnet")
	if err != nil {
		return oneXMLNode{}, false, err
	}
	item, ok := oneResolveByName(pool, "VNET", name)
	return item, ok, nil
}

func oneVnetResultWithFacts(res Result, vnet oneXMLNode) Result {
	res = res.WithExtra("id", vnet.childInt("ID"))
	res = res.WithExtra("name", vnet.childText("NAME"))
	if t, ok := vnet.child("TEMPLATE"); ok {
		res = res.WithExtra("template", t.toMap())
	} else {
		res = res.WithExtra("template", map[string]any{})
	}
	res = res.WithExtra("user_name", vnet.childText("UNAME"))
	res = res.WithExtra("user_id", vnet.childInt("UID"))
	res = res.WithExtra("group_name", vnet.childText("GNAME"))
	res = res.WithExtra("group_id", vnet.childInt("GID"))
	if perms, ok := vnet.child("PERMISSIONS"); ok {
		res = res.WithExtra("permissions", map[string]any{
			"owner_u": perms.childText("OWNER_U"), "owner_m": perms.childText("OWNER_M"), "owner_a": perms.childText("OWNER_A"),
			"group_u": perms.childText("GROUP_U"), "group_m": perms.childText("GROUP_M"), "group_a": perms.childText("GROUP_A"),
			"other_u": perms.childText("OTHER_U"), "other_m": perms.childText("OTHER_M"), "other_a": perms.childText("OTHER_A"),
		})
	}
	res = res.WithExtra("clusters", oneChildIDList(vnet, "CLUSTERS"))
	res = res.WithExtra("bridge", vnet.childText("BRIDGE"))
	res = res.WithExtra("bride_type", vnet.childText("BRIDGE_TYPE")) // sic — see this file's own doc comment
	res = res.WithExtra("parent_network_id", vnet.childText("PARENT_NETWORK_ID"))
	res = res.WithExtra("vn_mad", vnet.childText("VN_MAD"))
	res = res.WithExtra("phydev", vnet.childText("PHYDEV"))
	res = res.WithExtra("vlan_id", vnet.childText("VLAN_ID"))
	res = res.WithExtra("outer_vlan_id", vnet.childText("OUTER_VLAN_ID"))
	res = res.WithExtra("used_leases", vnet.childText("USED_LEASES"))
	res = res.WithExtra("vrouters", oneChildIDList(vnet, "VROUTERS"))
	res = res.WithExtra("ar_pool", oneVnetARPool(vnet))
	return res
}

func oneVnetARPool(vnet oneXMLNode) []any {
	arPool, ok := vnet.child("AR_POOL")
	if !ok {
		return []any{}
	}
	var out []any
	for _, ar := range arPool.children("AR") {
		allocated := ar.childText("ALLOCATED")
		if allocated == "" {
			allocated = "Null"
		}
		ip := ar.childText("IP")
		if ip == "" {
			ip = "Null"
		}
		globalPrefix := ar.childText("GLOBAL_PREFIX")
		if globalPrefix == "" {
			globalPrefix = "Null"
		}
		parentNetworkARID := ar.childText("PARENT_NETWORK_AR_ID")
		if parentNetworkARID == "" {
			parentNetworkARID = "Null"
		}
		ulaPrefix := ar.childText("ULA_PREFIX")
		if ulaPrefix == "" {
			ulaPrefix = "Null"
		}
		vnMad := ar.childText("VN_MAD")
		if vnMad == "" {
			vnMad = "Null"
		}
		out = append(out, map[string]any{
			"ar_id": ar.childText("AR_ID"), "mac": ar.childText("MAC"), "size": ar.childText("SIZE"), "type": ar.childText("TYPE"),
			"allocated": allocated, "ip": ip, "global_prefix": globalPrefix,
			"parent_network_ar_id": parentNetworkARID, "ula_prefix": ulaPrefix, "vn_mad": vnMad,
		})
	}
	if out == nil {
		out = []any{}
	}
	return out
}
