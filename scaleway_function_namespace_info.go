package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayFunctionNamespaceInfo implements Ansible's
// `scaleway_function_namespace_info` (community.general) module:
// read-only lookup of a Scaleway Serverless Functions namespace by
// exact name, via `scw function namespace list` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_function_namespace_info's own direct
// REST API calls.
//
// Args, not-found handling (Fail(), matching real info_strategy's own
// fail_json), and Extra shape mirror
// scaleway_container_namespace_info.go's own module exactly — see that
// module's own doc comment — except this port shells out to `scw
// function namespace list` (Scaleway's Functions product) instead of
// `container namespace list`.
//
// Extra["function_namespace"]: `scw`'s own JSON object for the
// namespace.
func moduleScalewayFunctionNamespaceInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_function_namespace_info"); !ok {
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
	region, err := scwRegionArg(args, "scaleway_function_namespace_info")
	if err != nil {
		return Result{}, err
	}

	current, exists, err := scwFindByName(ctx, conn, name,
		"function", "namespace", "list", "project-id="+projectID, "region="+region)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("scaleway_function_namespace_info: no function namespace named " + name + " found in project " + projectID), nil
	}
	return Ok("").WithExtra("function_namespace", current), nil
}
