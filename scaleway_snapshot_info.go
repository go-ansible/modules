package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewaySnapshotInfo implements Ansible's
// `scaleway_snapshot_info` (community.general) module: lists every
// Scaleway block-storage volume snapshot in a zone, via `scw instance
// snapshot list` — see scaleway_common.go's own doc comment for why
// this port substitutes the `scw` CLI, and for the region->zone mapping
// (scwZone) used below.
//
// Args: region (required).
//
// Deviation — Extra key: real scaleway_snapshot_info returns its list
// under `scaleway_snapshot_info`; this port uses Extra["snapshots"]
// instead (see github_secrets_info.go's own Extra["secrets"] for the
// house-convention precedent).
func moduleScalewaySnapshotInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_snapshot_info"); !ok {
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
	res, err := scwRunJSON(ctx, conn, "instance", "snapshot", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("scaleway_snapshot_info: failed to list snapshots: " + scwErrMsg(res)), nil
	}
	var snapshots []map[string]any
	if derr := scwDecode(res.Stdout, &snapshots); derr != nil {
		return Result{}, derr
	}
	if snapshots == nil {
		snapshots = []map[string]any{}
	}
	return Ok("").WithExtra("snapshots", snapshots), nil
}
