package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayIPInfo implements Ansible's `scaleway_ip_info`
// (community.general) module: lists every Scaleway flexible IP in a
// zone, via `scw instance ip list` — see scaleway_common.go's own doc
// comment for why this port substitutes the `scw` CLI, and for the
// region->zone mapping (scwZone) used below.
//
// Args: region (required).
//
// Deviation — Extra key: real scaleway_ip_info returns its list under
// `scaleway_ip_info`; this port uses Extra["ips"] instead, matching
// this port's own house convention of a short resource-name key (see
// github_secrets_info.go's own Extra["secrets"] for the precedent).
func moduleScalewayIPInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_ip_info"); !ok {
		return res, nil
	}
	region, err := requireString(args, "region")
	if err != nil {
		return Result{}, err
	}
	zone, err := scwZone(region)
	if err != nil {
		return Result{}, err
	}
	res, err := scwRunJSON(ctx, conn, "instance", "ip", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("scaleway_ip_info: failed to list IPs: " + scwErrMsg(res)), nil
	}
	var ips []map[string]any
	if derr := scwDecode(res.Stdout, &ips); derr != nil {
		return Result{}, derr
	}
	if ips == nil {
		ips = []map[string]any{}
	}
	return Ok("").WithExtra("ips", ips), nil
}
