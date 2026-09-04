package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayContainerNamespaceInfo implements Ansible's
// `scaleway_container_namespace_info` (community.general) module:
// read-only lookup of a Scaleway Serverless Containers namespace by
// exact name, via `scw container namespace list` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_container_namespace_info's own direct
// REST API calls.
//
// Args: name (required); project_id (required); region (required,
// fr-par|nl-ams|pl-waw); api_token/api_url/profile/api_timeout/
// validate_certs/query_parameters accepted, no effect.
//
// Real scaleway_container_namespace_info's own info_strategy fetches
// every namespace in project_id/region and fail_json()s if none match
// name exactly — this port matches that: Fail(), not an empty/absent
// result, when no namespace named name exists (see
// scaleway_container_info.go's own doc comment for the identical
// stance on scaleway_container_info, verified directly against
// scaleway_container_namespace_info.py's own info_strategy).
//
// Extra["container_namespace"]: `scw`'s own JSON object for the
// namespace.
func moduleScalewayContainerNamespaceInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_container_namespace_info"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	projectID, err := requireString(args, "project_id")
	if err != nil {
		return Result{}, err
	}
	region, err := scwRegionArg(args, "scaleway_container_namespace_info")
	if err != nil {
		return Result{}, err
	}

	current, exists, err := scwFindByName(ctx, conn, name,
		"container", "namespace", "list", "project-id="+projectID, "region="+region)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("scaleway_container_namespace_info: no container namespace named " + name + " found in project " + projectID), nil
	}
	return Ok("").WithExtra("container_namespace", current), nil
}
