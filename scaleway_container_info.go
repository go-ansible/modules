package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayContainerInfo implements Ansible's
// `scaleway_container_info` (community.general) module: read-only
// lookup of a Scaleway Serverless Container by exact name within a
// namespace, via `scw container container list` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_container_info's own direct REST API
// calls.
//
// Args: namespace_id (required); name (required); region (required,
// fr-par|nl-ams|pl-waw); api_token/api_url/profile/api_timeout/
// validate_certs/query_parameters accepted, no effect.
//
// Real scaleway_container_info's own info_strategy fetches every
// container in namespace_id and fail_json()s if none match name
// exactly — this port matches that: Fail(), not an empty/absent
// result, when no container named name exists (verified directly
// against scaleway_container_info.py's own info_strategy).
//
// Extra["container"]: `scw`'s own JSON object for the container.
func moduleScalewayContainerInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_container_info"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	namespaceID, err := requireString(args, "namespace_id")
	if err != nil {
		return Result{}, err
	}
	region, err := scwRegionArg(args, "scaleway_container_info")
	if err != nil {
		return Result{}, err
	}

	current, exists, err := scwFindByName(ctx, conn, name,
		"container", "container", "list", "namespace-id="+namespaceID, "region="+region)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("scaleway_container_info: no container named " + name + " found in namespace " + namespaceID), nil
	}
	return Ok("").WithExtra("container", current), nil
}
