package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIbmSaPool implements Ansible's `ibm_sa_pool` module: creates or
// deletes a storage pool on an IBM Spectrum Accelerate Family system,
// via `xcli` (see ibm_sa_common.go's own doc comment).
//
// Args: pool (required); state (present|absent, default present); size,
// snapshot_size, domain, perf_class — optional, forwarded as
// `pool_create`'s own xcli fields (matching real ibm_sa_pool.py's own
// argument_spec — its own EXAMPLES use `size: 300` for a pool's hard
// size in GB, matching xcli's own `pool_create ... hard_size=171
// soft_size=171 snapshot_size=65`-shaped documented example, though real
// ibm_sa_pool.py itself forwards this module's own `size` argument
// verbatim as pyxcli's `size` keyword rather than renaming it to xcli's
// own `hard_size` field name — this port matches that real behavior
// exactly rather than "fixing" it, since xcli genuinely accepts a `size`
// alias per its own command help in addition to `hard_size`/`soft_size`).
//
// Idempotency: `pool_list pool=<name>` decides existence; state=present
// creates only if absent, state=absent deletes only if present —
// matching real ibm_sa_pool.py exactly.
func moduleIbmSaPool(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	pool, err := requireString(args, "pool")
	if err != nil {
		return Result{}, err
	}
	if res, ok := ibmSaRequireBinary(ctx, conn, "ibm_sa_pool", args); !ok {
		return res, nil
	}
	state := argString(args, "state", "present")

	rows, ok, err := ibmSaList(ctx, conn, args, "pool_list", map[string]string{"pool": pool})
	if err != nil {
		return Result{}, err
	}
	exists := ok && len(rows) > 0

	switch state {
	case "present":
		if exists {
			return Ok(""), nil
		}
		fields := ibmSaFieldsFromArgs(args, "pool", "size", "snapshot_size", "domain", "perf_class")
		res, err := ibmSaRun(ctx, conn, args, "pool_create", fields)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(""), nil

	case "absent":
		if !exists {
			return Ok(""), nil
		}
		res, err := ibmSaRun(ctx, conn, args, "pool_delete", map[string]string{"pool": pool})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(""), nil

	default:
		return Result{}, errArg("ibm_sa_pool: state must be present or absent, got %q", state)
	}
}
