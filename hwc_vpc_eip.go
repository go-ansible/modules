package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcVpcEip implements Ansible's `hwc_vpc_eip`
// (community.general) module: creates or deletes a Huawei Cloud
// elastic IP address — see hwc_common.go's own doc comment for the
// KooCLI substitution shared by every hwc_* module in this batch.
// Operation IDs (CreatePublicip/ShowPublicip/DeletePublicip/
// ListPublicips, KooCLI service code "VPC") are DERIVED from real
// hwc_vpc_eip.py's own REST path ("publicips/{id}", read before
// implementing), following hwc_common.go's own confirmed
// PascalCase(Verb+Resource) convention.
//
// Args: type (required — the EIP type/line, e.g. "5_bgp", passed
// through verbatim, not validated against a choices list this port
// could not confirm live); dedicated_bandwidth (dict:
// charge_mode/name/size, optional); enterprise_project_id,
// ip_version, ipv4_address, port_id, shared_bandwidth_id (all
// optional); id; region; state (present|absent, default present).
//
// Real hwc_vpc_eip.py has NO natural non-id selector field at all (no
// `name` argument exists on this resource) — this port therefore only
// EVER looks an EIP up by id (see hwc_common.go's own doc comment on
// hwcFindByIDOrSelector's empty-selector behavior); with no id given,
// state=present always creates a NEW elastic IP (there is nothing
// else this port, or the real module, could compare against), and
// state=absent with no id given is always a no-op.
//
// Deviation — request body shape: real Huawei's EIP-creation body
// splits fields across two top-level objects, "publicip" (type,
// ip_version, ip_address) and "bandwidth" (name, size, charge_mode,
// and either a fresh spec or shared_bandwidth_id to attach to an
// existing shared bandwidth) — this port maps ipv4_address into
// publicip.ip_address and dedicated_bandwidth's own three fields plus
// shared_bandwidth_id into bandwidth.*, its best-effort reconstruction
// of that split from real hwc_vpc_eip.py's own argument_spec, since
// this port had no live tenant to confirm the exact body shape
// against.
//
// Extra["id"]/Extra["publicip"]: as returned by KooCLI, present
// whenever the EIP now exists.
func moduleHwcVpcEip(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_vpc_eip"); !ok {
		return res, nil
	}
	eipType, err := requireString(args, "type")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_vpc_eip: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)
	id := argString(args, "id", "")

	match, found, _, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowPublicip", "ListPublicips", "publicip_id", id, nil, region)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_vpc_eip: already absent"), nil
		}
		rid := fmt.Sprint(match["id"])
		dres, err := hcloudRun(ctx, conn, "VPC", "DeletePublicip", mergeParams(map[string]string{"publicip_id": rid}, region))
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_vpc_eip", "deleting eip "+rid, dres), nil
		}
		return Changed("hwc_vpc_eip: "+rid+" deleted").WithExtra("id", rid), nil
	}

	if found {
		return Ok("hwc_vpc_eip: already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("publicip", match), nil
	}
	if id != "" {
		return Fail("hwc_vpc_eip: id " + id + " not found and state=present cannot create a specific id"), nil
	}

	cparams := map[string]string{"publicip.type": eipType}
	if v := argInt(args, "ip_version", 0); v != 0 {
		cparams["publicip.ip_version"] = fmt.Sprint(v)
	}
	if v := argString(args, "ipv4_address", ""); v != "" {
		cparams["publicip.ip_address"] = v
	}
	if v := argString(args, "enterprise_project_id", ""); v != "" {
		cparams["enterprise_project_id"] = v
	}
	if v := argString(args, "port_id", ""); v != "" {
		cparams["publicip.port_id"] = v
	}
	if v := argString(args, "shared_bandwidth_id", ""); v != "" {
		cparams["bandwidth.id"] = v
	}
	if bw, ok := args["dedicated_bandwidth"].(map[string]any); ok {
		if v := argString(bw, "charge_mode", ""); v != "" {
			cparams["bandwidth.charge_mode"] = v
		}
		if v := argString(bw, "name", ""); v != "" {
			cparams["bandwidth.name"] = v
		}
		if v := argInt(bw, "size", 0); v != 0 {
			cparams["bandwidth.size"] = fmt.Sprint(v)
		}
	}
	cparams = mergeParams(cparams, region)

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreatePublicip", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_vpc_eip", "creating eip", cres), nil
	}
	eip, _ := created["publicip"].(map[string]any)
	r := Changed("hwc_vpc_eip: created")
	if eip != nil {
		r = r.WithExtra("id", fmt.Sprint(eip["id"])).WithExtra("publicip", eip)
	}
	return r, nil
}
