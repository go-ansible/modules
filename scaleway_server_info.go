package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayServerInfo implements Ansible's `scaleway_server_info`
// (community.general) module: lists every Scaleway instance server in
// a zone, via `scw instance server list` — see scaleway_common.go's own
// doc comment for why this port substitutes the `scw` CLI, and for the
// region->zone mapping (scwZone) used below.
//
// Args: region (required).
//
// Deviation — Extra key: real scaleway_server_info returns its list
// under `scaleway_server_info`; this port uses Extra["servers"] instead
// (see github_secrets_info.go's own Extra["secrets"] for the
// house-convention precedent). Deviation — field shape: real
// scaleway_server_info's own RETURN VALUES sample is the FULL legacy
// v1 Instance API server resource (bootscript/image/location/
// maintenances/security_group/volumes as a numbered-key map, and more)
// — `scw instance server list -o json`'s own field set is whatever the
// current Scaleway Go SDK's Server struct marshals to, which this port
// has no live binary in this sandbox to compare field-by-field against
// (see scaleway_common.go's own "Output shape" caveat); this port
// passes that JSON through as-is rather than attempting to reshape it
// into the legacy sample's exact structure.
func moduleScalewayServerInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_server_info"); !ok {
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
	res, err := scwRunJSON(ctx, conn, "instance", "server", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("scaleway_server_info: failed to list servers: " + scwErrMsg(res)), nil
	}
	var servers []map[string]any
	if derr := scwDecode(res.Stdout, &servers); derr != nil {
		return Result{}, derr
	}
	if servers == nil {
		servers = []map[string]any{}
	}
	return Ok("").WithExtra("servers", servers), nil
}
