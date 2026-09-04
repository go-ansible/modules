package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewaySecurityGroupInfo implements Ansible's
// `scaleway_security_group_info` (community.general) module: lists
// every Scaleway instance security group in a zone, via `scw instance
// security-group list` — see scaleway_common.go's own doc comment for
// why this port substitutes the `scw` CLI, and for the region->zone
// mapping (scwZone) used below.
//
// Args: region (required).
//
// Deviation — Extra key: real scaleway_security_group_info returns its
// list under `scaleway_security_group_info`; this port uses
// Extra["security_groups"] instead (see github_secrets_info.go's own
// Extra["secrets"] for the house-convention precedent).
func moduleScalewaySecurityGroupInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_security_group_info"); !ok {
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
	res, err := scwRunJSON(ctx, conn, "instance", "security-group", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("scaleway_security_group_info: failed to list security groups: " + scwErrMsg(res)), nil
	}
	var groups []map[string]any
	if derr := scwDecode(res.Stdout, &groups); derr != nil {
		return Result{}, derr
	}
	if groups == nil {
		groups = []map[string]any{}
	}
	return Ok("").WithExtra("security_groups", groups), nil
}
