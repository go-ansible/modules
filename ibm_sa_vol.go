package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIbmSaVol implements Ansible's `ibm_sa_vol` module: creates or
// deletes a volume on an IBM Spectrum Accelerate Family system, via
// `xcli` (see ibm_sa_common.go's own doc comment).
//
// Args: vol (required); pool, size — optional, forwarded as
// `vol_create`'s own xcli fields (matching real ibm_sa_vol.py's own
// argument_spec and xcli's own documented `vol_create vol=<name>
// size=<n> pool=<name>` syntax); state (present|absent, default
// present).
//
// Idempotency: `vol_list vol=<name>` decides existence; state=present
// creates only if absent, state=absent deletes only if present —
// matching real ibm_sa_vol.py exactly.
func moduleIbmSaVol(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	vol, err := requireString(args, "vol")
	if err != nil {
		return Result{}, err
	}
	if res, ok := ibmSaRequireBinary(ctx, conn, "ibm_sa_vol", args); !ok {
		return res, nil
	}
	state := argString(args, "state", "present")

	rows, ok, err := ibmSaList(ctx, conn, args, "vol_list", map[string]string{"vol": vol})
	if err != nil {
		return Result{}, err
	}
	exists := ok && len(rows) > 0

	switch state {
	case "present":
		if exists {
			return Ok(""), nil
		}
		fields := ibmSaFieldsFromArgs(args, "vol", "pool", "size")
		res, err := ibmSaRun(ctx, conn, args, "vol_create", fields)
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
		res, err := ibmSaRun(ctx, conn, args, "vol_delete", map[string]string{"vol": vol})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(""), nil

	default:
		return Result{}, errArg("ibm_sa_vol: state must be present or absent, got %q", state)
	}
}
