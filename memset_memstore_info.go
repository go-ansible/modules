package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMemsetMemstoreInfo implements Ansible's `memset_memstore_info`
// module via Memset's own official `ma-shell` — see memset_common.go's
// own doc comment for the CLI-substitution rationale and ma-shell's own
// invocation syntax.
//
// Args: api_key (string, required, unavoidably on argv — see
// memset_common.go's own doc comment); name (string, required) — the
// Memstore product name.
//
// RPC method `memstore.usage` (param: name), verified directly in real
// memset_memstore_info.py's own source. This is a read-only info
// operation (real module's own check_mode support is "full", but since
// this call never mutates anything there is no behavior to skip either
// way — this port always performs the same single read).
func moduleMemsetMemstoreInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	apiKey, err := requireString(args, "api_key")
	if err != nil {
		return Result{}, errArg("memset_memstore_info: %v", err)
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, errArg("memset_memstore_info: %v", err)
	}

	if res, ok := msRequireBinary(ctx, conn, "memset_memstore_info"); !ok {
		return res, nil
	}

	result, problem, err := msCall(ctx, conn, apiKey, "memstore.usage", []msParam{msStr("name", name)})
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_memstore_info: memstore.usage: %s", problem)), nil
	}
	return Ok(fmt.Sprintf("fetched Memstore usage for %s", name)).WithExtra("memset_api", msObject(result)), nil
}
