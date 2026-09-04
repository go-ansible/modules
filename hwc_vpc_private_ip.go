package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcVpcPrivateIp implements Ansible's `hwc_vpc_private_ip`
// (community.general) module: reserves or releases one private IP
// address on a Huawei Cloud VPC subnet — see hwc_common.go's own doc
// comment for the KooCLI substitution shared by every hwc_* module in
// this batch. Operation IDs (CreatePrivateIP/ShowPrivateIP/
// DeletePrivateIP/ListPrivateIPs, KooCLI service code "VPC") are
// DERIVED from real hwc_vpc_private_ip.py's own REST paths
// ("subnets/{subnet_id}/privateips" for create/list, "privateips/{id}"
// for show/delete — read before implementing), following
// hwc_common.go's own confirmed PascalCase(Verb+Resource) convention.
//
// Args: subnet_id (required); ip_address (optional — the system
// assigns one automatically when omitted, matching real
// hwc_vpc_private_ip.py's own documented behavior); id (takes
// precedence for lookup); state (present|absent, default present).
// This module has no `region` argument in its own real argument_spec
// (unlike its sibling hwc_* modules), so none is accepted here either.
//
// Lookup: id given -> ShowPrivateIP; else ListPrivateIPs filtered
// client-side by subnet_id + ip_address (when ip_address given — real
// hwc_vpc_private_ip.py's own NOTES document exactly this pair:
// "`subnet_id', `ip_address' are used for private IP selection").
// Without ip_address there is nothing to select on beyond subnet_id
// alone: if the subnet holds zero or exactly one private IP already,
// that is unambiguous and this port proceeds; if it holds more than
// one, this port Fails naming the ambiguity, matching real
// hwc_vpc_private_ip.py's own "execution is aborted" NOTE for this
// case. state=present on an
// already-found private IP is always a no-op (see hwc_common.go's own
// doc comment on this batch's uniform no-update simplification — real
// hwc_vpc_private_ip.py's own NOTES independently confirm this
// resource never supports update either).
//
// Extra["id"]/Extra["private_ip"]: as returned by KooCLI, present
// whenever the private IP now exists.
func moduleHwcVpcPrivateIp(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_vpc_private_ip"); !ok {
		return res, nil
	}
	subnetID, err := requireString(args, "subnet_id")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_vpc_private_ip: state must be one of present, absent, got %q", state)
	}
	ipAddress := argString(args, "ip_address", "")
	id := argString(args, "id", "")
	selector := map[string]string{"subnet_id": subnetID}
	if ipAddress != "" {
		selector["ip_address"] = ipAddress
	}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowPrivateIP", "ListPrivateIPs",
		"privateip_id", id, selector, nil)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail("hwc_vpc_private_ip: more than one private IP matches subnet_id=" + subnetID +
			"; specify ip_address (or id) to disambiguate — execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_vpc_private_ip: already absent"), nil
		}
		rid := fmt.Sprint(match["id"])
		dres, err := hcloudRun(ctx, conn, "VPC", "DeletePrivateIP", map[string]string{"privateip_id": rid})
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_vpc_private_ip", "deleting private ip "+rid, dres), nil
		}
		return Changed("hwc_vpc_private_ip: "+rid+" deleted").WithExtra("id", rid), nil
	}

	if found {
		return Ok("hwc_vpc_private_ip: already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("private_ip", match), nil
	}

	cparams := map[string]string{"privateips.[0].subnet_id": subnetID}
	if ipAddress != "" {
		cparams["privateips.[0].ip_address"] = ipAddress
	}

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreatePrivateIP", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_vpc_private_ip", "creating private ip", cres), nil
	}
	var ip map[string]any
	if arr := hcloudListArray(created); len(arr) > 0 {
		ip = arr[0]
	} else if pi, ok := created["privateip"].(map[string]any); ok {
		ip = pi
	}
	r := Changed("hwc_vpc_private_ip: created")
	if ip != nil {
		r = r.WithExtra("id", fmt.Sprint(ip["id"])).WithExtra("private_ip", ip)
	}
	return r, nil
}
