package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcVpcPort implements Ansible's `hwc_vpc_port`
// (community.general) module: creates or deletes a Neutron-compatible
// port on a Huawei Cloud VPC subnet — see hwc_common.go's own doc
// comment for the KooCLI substitution shared by every hwc_* module in
// this batch. Operation IDs (CreatePort/ShowPort/DeletePort/ListPorts,
// KooCLI service code "VPC") are DERIVED from real hwc_vpc_port.py's
// own REST path ("ports/{id}", read before implementing), following
// hwc_common.go's own confirmed PascalCase(Verb+Resource) convention.
//
// Args: subnet_id (required); admin_state_up (bool), ip_address, name,
// security_groups (list of IDs), allowed_address_pairs (list of dict:
// ip_address/mac_address), extra_dhcp_opts (list of dict: name/value)
// — all optional; id (takes precedence for lookup); region; state
// (present|absent, default present).
//
// Lookup: id given -> ShowPort; else ListPorts filtered client-side by
// subnet_id + name (when name given). state=present on an
// already-found port is always a no-op (see hwc_common.go's own doc
// comment on this batch's uniform no-update simplification).
//
// List-valued fields (security_groups, allowed_address_pairs,
// extra_dhcp_opts) are sent using KooCLI's own documented dot-notation
// array addressing (port.security_groups.[N], etc. — see
// hwc_common.go's own doc comment on this syntax, confirmed from
// KooCLI's own parameter-reference docs).
//
// Extra["id"]/Extra["port"]: as returned by KooCLI, present whenever
// the port now exists.
func moduleHwcVpcPort(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_vpc_port"); !ok {
		return res, nil
	}
	subnetID, err := requireString(args, "subnet_id")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_vpc_port: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)
	name := argString(args, "name", "")
	selector := map[string]string{"subnet_id": subnetID}
	if name != "" {
		selector["name"] = name
	}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowPort", "ListPorts", "port_id",
		argString(args, "id", ""), selector, region)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail("hwc_vpc_port: more than one port matches the given selector; execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_vpc_port: already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := hcloudRun(ctx, conn, "VPC", "DeletePort", mergeParams(map[string]string{"port_id": id}, region))
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_vpc_port", "deleting port "+id, dres), nil
		}
		return Changed("hwc_vpc_port: "+id+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_vpc_port: already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("port", match), nil
	}

	cparams := map[string]string{"port.subnet_id": subnetID}
	if name != "" {
		cparams["port.name"] = name
	}
	if v := argString(args, "ip_address", ""); v != "" {
		cparams["port.fixed_ips.[0].ip_address"] = v
	}
	if _, ok := args["admin_state_up"]; ok {
		cparams["port.admin_state_up"] = fmt.Sprint(argBool(args, "admin_state_up", true))
	}
	for i, sg := range argStringList(args, "security_groups") {
		cparams[fmt.Sprintf("port.security_groups.[%d]", i)] = sg
	}
	if pairs, ok := args["allowed_address_pairs"].([]any); ok {
		for i, p := range pairs {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if v := argString(pm, "ip_address", ""); v != "" {
				cparams[fmt.Sprintf("port.allowed_address_pairs.[%d].ip_address", i)] = v
			}
			if v := argString(pm, "mac_address", ""); v != "" {
				cparams[fmt.Sprintf("port.allowed_address_pairs.[%d].mac_address", i)] = v
			}
		}
	}
	if opts, ok := args["extra_dhcp_opts"].([]any); ok {
		for i, o := range opts {
			om, ok := o.(map[string]any)
			if !ok {
				continue
			}
			if v := argString(om, "name", ""); v != "" {
				cparams[fmt.Sprintf("port.extra_dhcp_opts.[%d].opt_name", i)] = v
			}
			if v := argString(om, "value", ""); v != "" {
				cparams[fmt.Sprintf("port.extra_dhcp_opts.[%d].opt_value", i)] = v
			}
		}
	}
	cparams = mergeParams(cparams, region)

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreatePort", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_vpc_port", "creating port", cres), nil
	}
	port, _ := created["port"].(map[string]any)
	r := Changed("hwc_vpc_port: created")
	if port != nil {
		r = r.WithExtra("id", fmt.Sprint(port["id"])).WithExtra("port", port)
	}
	return r, nil
}
