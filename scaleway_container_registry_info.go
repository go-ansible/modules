package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayContainerRegistryInfo implements Ansible's
// `scaleway_container_registry_info` (community.general) module:
// read-only lookup of a Scaleway Container Registry namespace by exact
// name, via `scw registry namespace list` — see scaleway_common.go's
// own doc comment for why this port substitutes the `scw` CLI for real
// scaleway_container_registry_info's own direct REST API calls.
//
// Args: name (required); project_id (required); region (required,
// fr-par|nl-ams|pl-waw); api_token/api_url/profile/api_timeout/
// validate_certs/query_parameters accepted, no effect.
//
// Not-found handling (Fail(), matching real info_strategy's own
// fail_json when no registry named name exists) mirrors
// scaleway_container_namespace_info.go's own module — see that
// module's own doc comment.
//
// Extra["container_registry"]: `scw`'s own JSON object for the
// registry namespace.
func moduleScalewayContainerRegistryInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_container_registry_info"); !ok {
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
	region, err := scwRegionArg(args, "scaleway_container_registry_info")
	if err != nil {
		return Result{}, err
	}

	current, exists, err := scwFindByName(ctx, conn, name,
		"registry", "namespace", "list", "project-id="+projectID, "region="+region)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("scaleway_container_registry_info: no container registry named " + name + " found in project " + projectID), nil
	}
	return Ok("").WithExtra("container_registry", current), nil
}
