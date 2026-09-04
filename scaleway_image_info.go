package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayImageInfo implements Ansible's `scaleway_image_info`
// (community.general) module: lists every Scaleway Instance image
// available in a zone, via `scw instance image list` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_image_info's own direct REST API
// calls, and for the zone-translation deviation shared by
// scaleway_compute/scaleway_compute_private_network (real
// scaleway_image_info's own `region` argument is INSTANCE-API-zone-
// shaped, not the container/function-family fr-par/nl-ams/pl-waw
// shape — see scwZone).
//
// Args: region (required); api_token/api_url/profile/api_timeout/
// validate_certs/query_parameters accepted, no effect.
//
// Real scaleway_image_info has no name/filter argument at all — it
// always returns every image visible in the zone (the caller's own
// organization's private images plus Scaleway's own public ones),
// matching this port's own unfiltered `scw instance image list
// zone=...` exactly. Always Changed=false (a pure read).
//
// Extra["scaleway_image_info"]: `scw`'s own JSON array of image
// objects — matching real scaleway_image_info's own identically-named
// return key (a list, not a dict — this is the one scaleway_* module
// in this batch whose own return value is a bare array, verified
// against its own RETURN VALUES sample).
func moduleScalewayImageInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_image_info"); !ok {
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

	res, err := scwRunJSON(ctx, conn, "instance", "image", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("scaleway_image_info: failed to list images: " + scwErrMsg(res)), nil
	}
	var images []map[string]any
	if derr := scwDecode(res.Stdout, &images); derr != nil {
		return Result{}, derr
	}
	if images == nil {
		images = []map[string]any{}
	}
	return Ok("").WithExtra("scaleway_image_info", images), nil
}
