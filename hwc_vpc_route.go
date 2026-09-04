package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcVpcRoute implements Ansible's `hwc_vpc_route`
// (community.general) module: creates or deletes a Huawei Cloud VPC
// route — see hwc_common.go's own doc comment for the KooCLI
// substitution shared by every hwc_* module in this batch. Operation
// IDs (CreateVpcRoute/ShowVpcRoute/DeleteVpcRoute/ListVpcRoutes,
// KooCLI service code "VPC") are DERIVED from real hwc_vpc_route.py's
// own REST path ("v2.0/vpc/routes/{id}", read before implementing),
// following hwc_common.go's own confirmed PascalCase(Verb+Resource)
// convention; the request-body field name for next_hop
// ("route.nexthop", one word, matching OpenStack Neutron's own
// extraroute naming rather than an underscored "next_hop") is this
// port's best-effort guess from that same convention, not
// independently confirmed against a live tenant.
//
// Args: destination, next_hop, vpc_id (required); type (default
// "peering"); id (takes precedence for lookup); state (present|absent,
// default present). This module has no `region` argument in its own
// real argument_spec, so none is accepted here either.
//
// Lookup: id given -> ShowVpcRoute; else ListVpcRoutes filtered
// client-side by destination + vpc_id + type + next_hop — real
// hwc_vpc_route.py's own NOTES document exactly this quadruple for
// route selection. state=present on an already-found route is always
// a no-op (see hwc_common.go's own doc comment on this batch's uniform
// no-update simplification — real hwc_vpc_route.py's own NOTES
// independently confirm this resource never supports update either).
//
// Extra["id"]/Extra["route"]: as returned by KooCLI, present whenever
// the route now exists.
func moduleHwcVpcRoute(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_vpc_route"); !ok {
		return res, nil
	}
	destination, err := requireString(args, "destination")
	if err != nil {
		return Result{}, err
	}
	nextHop, err := requireString(args, "next_hop")
	if err != nil {
		return Result{}, err
	}
	vpcID, err := requireString(args, "vpc_id")
	if err != nil {
		return Result{}, err
	}
	routeType := argString(args, "type", "peering")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_vpc_route: state must be one of present, absent, got %q", state)
	}
	selector := map[string]string{"destination": destination, "vpc_id": vpcID, "type": routeType, "nexthop": nextHop}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowVpcRoute", "ListVpcRoutes",
		"route_id", argString(args, "id", ""), selector, nil)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail(fmt.Sprintf("hwc_vpc_route: more than one route matches destination=%s vpc_id=%s type=%s next_hop=%s; execution aborted", destination, vpcID, routeType, nextHop)), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_vpc_route: already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := hcloudRun(ctx, conn, "VPC", "DeleteVpcRoute", map[string]string{"route_id": id})
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_vpc_route", "deleting route "+id, dres), nil
		}
		return Changed("hwc_vpc_route: "+id+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_vpc_route: already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("route", match), nil
	}

	cparams := map[string]string{
		"route.destination": destination, "route.vpc_id": vpcID, "route.type": routeType, "route.nexthop": nextHop,
	}

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreateVpcRoute", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_vpc_route", "creating route", cres), nil
	}
	route, _ := created["route"].(map[string]any)
	r := Changed("hwc_vpc_route: created")
	if route != nil {
		r = r.WithExtra("id", fmt.Sprint(route["id"])).WithExtra("route", route)
	}
	return r, nil
}
