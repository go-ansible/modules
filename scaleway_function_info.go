package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayFunctionInfo implements Ansible's
// `scaleway_function_info` (community.general) module: read-only
// lookup of a Scaleway Serverless Function by exact name within a
// namespace, via `scw function function list` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_function_info's own direct REST API
// calls.
//
// Args: namespace_id (required — real scaleway_function_info's own doc
// text mislabels this "Container namespace identifier", but its
// argument_spec and this module's own `scw function function list`
// backing are unambiguously Functions-namespace-scoped, verified
// against scaleway_function_info.py directly); name (required); region
// (required, fr-par|nl-ams|pl-waw); api_token/api_url/profile/
// api_timeout/validate_certs/query_parameters accepted, no effect.
//
// Not-found handling (Fail(), matching real info_strategy's own
// fail_json) mirrors scaleway_container_info.go's own module — see that
// module's own doc comment.
//
// Extra["function"]: `scw`'s own JSON object for the function.
func moduleScalewayFunctionInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_function_info"); !ok {
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
	region, err := scwRegionArg(args, "scaleway_function_info")
	if err != nil {
		return Result{}, err
	}

	current, exists, err := scwFindByName(ctx, conn, name,
		"function", "function", "list", "namespace-id="+namespaceID, "region="+region)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("scaleway_function_info: no function named " + name + " found in namespace " + namespaceID), nil
	}
	return Ok("").WithExtra("function", current), nil
}
