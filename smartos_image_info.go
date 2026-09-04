package modules

import (
	"context"
	"encoding/json"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSmartosImageInfo implements Ansible's `smartos_image_info`
// (community.general) module: read-only facts about every image
// installed on a SmartOS host, via `imgadm list -j`.
//
// Args: filters (string, optional) — a free-form filter expression
// (e.g. "os=linux state=active public=false") appended to `imgadm
// list -j`, matching real smartos_image_info's own filters passthrough
// (see `imgadm list` under https://smartos.org/man/8/imgadm for the
// expression syntax).
//
// Extra["smartos_images"]: a map keyed by image UUID, each value being
// that image's full manifest (as `imgadm list -j` reports it) merged
// with "clones", "source", and "zpool" — matching real
// smartos_image_info's own return shape
// (`module.exit_json(smartos_images=...)`), though this port surfaces
// it under Result.Extra rather than as a bare top-level return key,
// matching every other module in this batch.
//
// Deviation from real smartos_image_info: real smartos_image_info's
// own return_all_installed_images(), on an `imgadm list -j` failure,
// calls `module.exit_json(msg=..., stderr=err)` — exit_json, not
// fail_json — so a failed listing is still reported as a SUCCESSFUL,
// unchanged run carrying only a msg/stderr field, with no
// smartos_images key at all and no indication in Ansible's own
// changed/failed accounting that anything went wrong. Nothing in real
// smartos_image_info's own RETURN VALUES documents this path, so it
// reads as an oversight rather than intended behavior; this port
// reproduces it as-is anyway (an Ok(), not a Fail(), result carrying
// msg+stderr and no smartos_images key) since replicating real
// behavior exactly is this batch's own stated priority over guessing
// improved behavior.
//
// Never changed — this module only ever reads. real
// smartos_image_info also passes `filters` through as a single argv
// token exactly as given (never shell-split), which this port matches
// by splicing it into the command line unquoted.
func moduleSmartosImageInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	filters := argString(args, "filters", "")

	cmd := "imgadm list -j"
	if filters != "" {
		cmd += " " + filters
	}
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Ok("Failed to get all installed images").WithExtra("stderr", res.Stderr), nil
	}

	var images []map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &images); err != nil {
		return Result{}, fmt.Errorf("smartos_image_info: parsing imgadm output: %w", err)
	}

	result := map[string]any{}
	for _, image := range images {
		manifest, _ := image["manifest"].(map[string]any)
		if manifest == nil {
			continue
		}
		uuid, _ := manifest["uuid"].(string)
		if uuid == "" {
			continue
		}
		for _, attr := range []string{"clones", "source", "zpool"} {
			manifest[attr] = image[attr]
		}
		result[uuid] = manifest
	}

	return Ok("").WithExtra("smartos_images", result), nil
}
