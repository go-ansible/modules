package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIbmSaHost implements Ansible's `ibm_sa_host` module: defines or
// removes a host object on an IBM Spectrum Accelerate Family system, via
// `xcli` (see ibm_sa_common.go's own doc comment).
//
// Args: host (required); state (present|absent, default present);
// cluster, domain, iscsi_chap_name, iscsi_chap_secret — optional,
// forwarded as `host_define`'s own xcli fields (matching real
// ibm_sa_host.py's own argument_spec). username/password/endpoints are
// the array connection arguments.
//
// Idempotency: `host_list host=<name>` decides existence; state=present
// defines only if absent (`host_define`), state=absent removes only if
// present (`host_delete`) — matching real ibm_sa_host.py exactly (no
// per-field update path either upstream or here).
func moduleIbmSaHost(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	host, err := requireString(args, "host")
	if err != nil {
		return Result{}, err
	}
	if res, ok := ibmSaRequireBinary(ctx, conn, "ibm_sa_host", args); !ok {
		return res, nil
	}
	state := argString(args, "state", "present")

	rows, ok, err := ibmSaList(ctx, conn, args, "host_list", map[string]string{"host": host})
	if err != nil {
		return Result{}, err
	}
	exists := ok && len(rows) > 0

	switch state {
	case "present":
		if exists {
			return Ok(""), nil
		}
		fields := ibmSaFieldsFromArgs(args, "host", "cluster", "domain", "iscsi_chap_name", "iscsi_chap_secret")
		res, err := ibmSaRun(ctx, conn, args, "host_define", fields)
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
		res, err := ibmSaRun(ctx, conn, args, "host_delete", map[string]string{"host": host})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(""), nil

	default:
		return Result{}, errArg("ibm_sa_host: state must be present or absent, got %q", state)
	}
}
