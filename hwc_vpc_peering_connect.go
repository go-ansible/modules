package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcVpcPeeringConnect implements Ansible's
// `hwc_vpc_peering_connect` (community.general) module: creates or
// deletes a VPC peering connection between two Huawei Cloud VPCs — see
// hwc_common.go's own doc comment for the KooCLI substitution shared
// by every hwc_* module in this batch. Operation IDs
// (CreateVpcPeering/ShowVpcPeering/DeleteVpcPeering/ListVpcPeerings,
// KooCLI service code "VPC") are DERIVED from real
// hwc_vpc_peering_connect.py's own REST path
// ("v2.0/vpc/peerings/{id}", read before implementing), following
// hwc_common.go's own confirmed PascalCase(Verb+Resource) convention.
//
// Args: local_vpc_id, name (required); peering_vpc (dict: vpc_id
// required, project_id optional — the peer side, possibly in a
// different Huawei Cloud project); description (optional); id (takes
// precedence for lookup); region; state (present|absent, default
// present).
//
// Lookup: id given -> ShowVpcPeering; else ListVpcPeerings filtered
// client-side by name + local_vpc_id. state=present on an
// already-found peering connection is always a no-op (see
// hwc_common.go's own doc comment on this batch's uniform no-update
// simplification).
//
// Deviation — request body shape: real Huawei's own peering-create
// body nests each side under its own object
// ("local_vpc_info"/"peer_vpc_info", each holding vpc_id and,
// peer-side only, tenant_id) — this port's best-effort reconstruction
// of that shape from real hwc_vpc_peering_connect.py's own
// argument_spec, not independently confirmed against a live tenant.
//
// Extra["id"]/Extra["peering"]: as returned by KooCLI, present
// whenever the connection now exists.
func moduleHwcVpcPeeringConnect(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_vpc_peering_connect"); !ok {
		return res, nil
	}
	localVpcID, err := requireString(args, "local_vpc_id")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	peeringVpc, ok := args["peering_vpc"].(map[string]any)
	if !ok {
		return Result{}, errArg("hwc_vpc_peering_connect: missing required argument: peering_vpc")
	}
	peerVpcID, err := requireString(peeringVpc, "vpc_id")
	if err != nil {
		return Result{}, errArg("hwc_vpc_peering_connect: peering_vpc.vpc_id: %v", err)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_vpc_peering_connect: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)
	selector := map[string]string{"name": name, "local_vpc_id": localVpcID}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowVpcPeering", "ListVpcPeerings",
		"peering_id", argString(args, "id", ""), selector, region)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail(fmt.Sprintf("hwc_vpc_peering_connect: more than one peering connection matches name=%s local_vpc_id=%s; execution aborted", name, localVpcID)), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_vpc_peering_connect: " + name + " already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := hcloudRun(ctx, conn, "VPC", "DeleteVpcPeering", mergeParams(map[string]string{"peering_id": id}, region))
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_vpc_peering_connect", "deleting peering "+id, dres), nil
		}
		return Changed("hwc_vpc_peering_connect: "+name+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_vpc_peering_connect: "+name+" already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("peering", match), nil
	}

	cparams := map[string]string{
		"peering.name": name, "peering.local_vpc_info.vpc_id": localVpcID, "peering.peer_vpc_info.vpc_id": peerVpcID,
	}
	if v := argString(peeringVpc, "project_id", ""); v != "" {
		cparams["peering.peer_vpc_info.tenant_id"] = v
	}
	if v := argString(args, "description", ""); v != "" {
		cparams["peering.description"] = v
	}
	cparams = mergeParams(cparams, region)

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreateVpcPeering", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_vpc_peering_connect", "creating peering "+name, cres), nil
	}
	peering, _ := created["peering"].(map[string]any)
	r := Changed("hwc_vpc_peering_connect: " + name + " created")
	if peering != nil {
		r = r.WithExtra("id", fmt.Sprint(peering["id"])).WithExtra("peering", peering)
	}
	return r, nil
}
