package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayVolumeInfo implements Ansible's `scaleway_volume_info`
// (community.general) module: lists every Scaleway block-storage volume
// in a zone, via `scw instance volume list` — see scaleway_common.go's
// own doc comment for why this port substitutes the `scw` CLI, and for
// the region->zone mapping (scwZone) used below.
//
// Args: region (required).
//
// Deviation — Extra key: real scaleway_volume_info returns its list
// under `scaleway_volume_info`; this port uses Extra["volumes"] instead
// (see github_secrets_info.go's own Extra["secrets"] for the
// house-convention precedent).
func moduleScalewayVolumeInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_volume_info"); !ok {
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
	res, err := scwRunJSON(ctx, conn, "instance", "volume", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("scaleway_volume_info: failed to list volumes: " + scwErrMsg(res)), nil
	}
	var volumes []map[string]any
	if derr := scwDecode(res.Stdout, &volumes); derr != nil {
		return Result{}, derr
	}
	if volumes == nil {
		volumes = []map[string]any{}
	}
	return Ok("").WithExtra("volumes", volumes), nil
}
