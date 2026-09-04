package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcVpcSubnet implements Ansible's `hwc_vpc_subnet`
// (community.general) module: creates or deletes a subnet inside a
// Huawei Cloud VPC — see hwc_common.go's own doc comment for the
// KooCLI substitution shared by every hwc_* module in this batch.
// Operation IDs (CreateSubnet/ShowSubnet/DeleteSubnet/ListSubnets,
// KooCLI service code "VPC") are DERIVED from real hwc_vpc_subnet.py's
// own REST path ("subnets/{id}", read before implementing — see that
// file's own send_create_request/send_delete_request), following the
// PascalCase(Verb+Resource) convention hwc_common.go's own doc comment
// confirmed elsewhere; not independently re-verified against a live
// KooCLI session.
//
// Args: cidr, gateway_ip, name, vpc_id (all required — real
// hwc_vpc_subnet.py requires them unconditionally, even for
// state=absent, a preserved upstream quirk); availability_zone
// (optional); dhcp_enable (bool, optional); dns_address (list of
// strings, optional — sent as subnet.dnsList.[N], KooCLI's own
// documented dot-notation array addressing); id (takes precedence for
// lookup); region; state (present|absent, default present).
//
// Lookup: id given -> ShowSubnet; else ListSubnets filtered
// client-side by vpc_id+name+cidr — real hwc_vpc_subnet.py's own NOTES
// document exactly this triple for subnet selection. state=present on
// an already-found subnet is always a no-op — see hwc_common.go's own
// doc comment on this batch's uniform no-update simplification (real
// hwc_vpc_subnet.py's own NOTES independently confirm this specific
// resource never supports update either: "No parameter support
// updating. If one of option is changed, the module creates a new
// resource").
//
// Extra["id"]/Extra["subnet"]: as returned by KooCLI, present whenever
// the subnet now exists.
func moduleHwcVpcSubnet(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_vpc_subnet"); !ok {
		return res, nil
	}
	cidr, err := requireString(args, "cidr")
	if err != nil {
		return Result{}, err
	}
	gatewayIP, err := requireString(args, "gateway_ip")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	vpcID, err := requireString(args, "vpc_id")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_vpc_subnet: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)
	selector := map[string]string{"vpc_id": vpcID, "name": name, "cidr": cidr}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowSubnet", "ListSubnets", "subnet_id",
		argString(args, "id", ""), selector, region)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail(fmt.Sprintf("hwc_vpc_subnet: more than one subnet matches vpc_id=%s name=%s cidr=%s; execution aborted", vpcID, name, cidr)), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_vpc_subnet: " + name + " already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := hcloudRun(ctx, conn, "VPC", "DeleteSubnet", mergeParams(map[string]string{"subnet_id": id, "vpc_id": vpcID}, region))
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_vpc_subnet", "deleting subnet "+id, dres), nil
		}
		return Changed("hwc_vpc_subnet: "+name+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_vpc_subnet: "+name+" already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("subnet", match), nil
	}

	cparams := map[string]string{
		"subnet.cidr": cidr, "subnet.gateway_ip": gatewayIP, "subnet.name": name, "subnet.vpc_id": vpcID,
	}
	if az := argString(args, "availability_zone", ""); az != "" {
		cparams["subnet.availability_zone"] = az
	}
	if _, ok := args["dhcp_enable"]; ok {
		cparams["subnet.dhcp_enable"] = fmt.Sprint(argBool(args, "dhcp_enable", true))
	}
	for i, dns := range argStringList(args, "dns_address") {
		cparams[fmt.Sprintf("subnet.dnsList.[%d]", i)] = dns
	}
	cparams = mergeParams(cparams, region)

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreateSubnet", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_vpc_subnet", "creating subnet "+name, cres), nil
	}
	subnet, _ := created["subnet"].(map[string]any)
	r := Changed("hwc_vpc_subnet: " + name + " created")
	if subnet != nil {
		r = r.WithExtra("id", fmt.Sprint(subnet["id"])).WithExtra("subnet", subnet)
	}
	return r, nil
}
