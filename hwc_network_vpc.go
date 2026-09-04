package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcNetworkVpc implements Ansible's `hwc_network_vpc`
// (community.general) module: creates or deletes a Huawei Cloud VPC —
// see hwc_common.go's own doc comment for the KooCLI (`hcloud`)
// substitution this shares with every other hwc_* module in this
// batch, including why identity_endpoint/user/password/domain/project
// are accepted but inert, why state=present on an already-found VPC is
// always a no-op (never a diff-and-update), and why operation IDs here
// (CreateVpc/ShowVpc/DeleteVpc/ListVpcs, under KooCLI's "VPC" service
// code) are independently confirmed against Huawei's own published API
// reference, not merely derived like several sibling hwc_* modules'
// own operation IDs.
//
// Args: name (required); cidr (required — real hwc_network_vpc.py
// requires it unconditionally too, even for state=absent, a preserved
// upstream quirk); id (the VPC's own resource ID — when given, takes
// precedence over name/cidr for lookup, matching real
// hwc_network_vpc.py's own documented NOTES); region (wired through as
// KooCLI's own --cli-region); timeouts (accepted, inert — this port's
// VPC create/delete calls are synchronous, so there is nothing to
// time out waiting for in the first place); state (present|absent,
// default present).
//
// Lookup: id given -> ShowVpc; else ListVpcs filtered client-side by
// name+cidr (hcloudFindOne) — more than one match is a Fail, matching
// every real hwc_* module's own "execution is aborted" NOTE.
//
// Extra["id"]: the VPC's own resource ID, present whenever the VPC now
// exists (created or already present). Extra["vpc"]: the VPC's raw
// JSON attributes as KooCLI itself returned them, present on create or
// when already present — this port exposes Huawei's own response
// object directly rather than flattening it into top-level Ansible
// facts the way real hwc_network_vpc's own HwcModule base class does,
// a documented, honest simplification (see hwc_common.go's own doc
// comment on this port's general Extra-field convention for this
// batch).
func moduleHwcNetworkVpc(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_network_vpc"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	cidr, err := requireString(args, "cidr")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_network_vpc: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowVpc", "ListVpcs", "vpc_id",
		argString(args, "id", ""), map[string]string{"name": name, "cidr": cidr}, region)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail(fmt.Sprintf("hwc_network_vpc: more than one VPC matches name=%s cidr=%s; execution aborted", name, cidr)), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_network_vpc: " + name + " already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dparams := mergeParams(map[string]string{"vpc_id": id}, region)
		dres, err := hcloudRun(ctx, conn, "VPC", "DeleteVpc", dparams)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_network_vpc", "deleting vpc "+id, dres), nil
		}
		return Changed("hwc_network_vpc: "+name+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_network_vpc: "+name+" already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("vpc", match), nil
	}

	cparams := mergeParams(map[string]string{"vpc.name": name, "vpc.cidr": cidr}, region)
	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreateVpc", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_network_vpc", "creating vpc "+name, cres), nil
	}
	vpc, _ := created["vpc"].(map[string]any)
	r := Changed("hwc_network_vpc: " + name + " created")
	if vpc != nil {
		r = r.WithExtra("id", fmt.Sprint(vpc["id"])).WithExtra("vpc", vpc)
	}
	return r, nil
}
