package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMemsetServerInfo implements Ansible's `memset_server_info`
// module via Memset's own official `ma-shell` — see memset_common.go's
// own doc comment for the CLI-substitution rationale and ma-shell's own
// invocation syntax.
//
// Args: api_key (string, required, unavoidably on argv — see
// memset_common.go's own doc comment); name (string, required) — the
// server product name.
//
// RPC method `server.info` (param: name), verified directly in real
// memset_server_info.py's own source — a read-only info operation, same
// note as memset_memstore_info.go's own doc comment on check_mode.
func moduleMemsetServerInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	apiKey, err := requireString(args, "api_key")
	if err != nil {
		return Result{}, errArg("memset_server_info: %v", err)
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, errArg("memset_server_info: %v", err)
	}

	if res, ok := msRequireBinary(ctx, conn, "memset_server_info"); !ok {
		return res, nil
	}

	result, problem, err := msCall(ctx, conn, apiKey, "server.info", []msParam{msStr("name", name)})
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_server_info: server.info: %s", problem)), nil
	}
	return Ok(fmt.Sprintf("fetched server info for %s", name)).WithExtra("memset_api", msObject(result)), nil
}
