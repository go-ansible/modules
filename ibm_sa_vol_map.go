package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIbmSaVolMap implements Ansible's `ibm_sa_vol_map` module: maps a
// volume to (or unmaps it from) a host or cluster on an IBM Spectrum
// Accelerate Family system, via `xcli` (see ibm_sa_common.go's own doc
// comment).
//
// Args: vol (required); state (present|absent, default present);
// cluster, host, lun, override — optional, forwarded as `map_vol`/
// `unmap_vol`'s own xcli fields (matching real ibm_sa_vol_map.py's own
// argument_spec).
//
// Idempotency matches real ibm_sa_vol_map.py's own logic EXACTLY,
// including its own narrow check: `vol_mapping_list vol=<name>` lists
// the volume's current mappings, and the volume is considered "already
// mapped" only if one of those mappings' own `host` field equals this
// task's `host` argument — real ibm_sa_vol_map.py performs this same
// comparison unconditionally (even for a cluster-only mapping request,
// where `host` is empty and every mapping's own host field is compared
// against ""), so a cluster mapping is never recognized as already
// present by this check either upstream or here; this port reproduces
// that real behavior rather than silently fixing it, per this project's
// own "faithfully reproduce an apparent upstream quirk rather than
// invent different behavior" convention (see aix_devices.go's own doc
// comment for the same stance).
func moduleIbmSaVolMap(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	vol, err := requireString(args, "vol")
	if err != nil {
		return Result{}, err
	}
	if res, ok := ibmSaRequireBinary(ctx, conn, "ibm_sa_vol_map", args); !ok {
		return res, nil
	}
	state := argString(args, "state", "present")
	host := argString(args, "host", "")

	rows, _, err := ibmSaList(ctx, conn, args, "vol_mapping_list", map[string]string{"vol": vol})
	if err != nil {
		return Result{}, err
	}
	mapped := false
	for _, row := range rows {
		if row["host"] == host {
			mapped = true
			break
		}
	}

	fields := ibmSaFieldsFromArgs(args, "vol", "lun", "cluster", "host", "override")

	switch state {
	case "present":
		if mapped {
			return Ok(""), nil
		}
		res, err := ibmSaRun(ctx, conn, args, "map_vol", fields)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(""), nil

	case "absent":
		if !mapped {
			return Ok(""), nil
		}
		res, err := ibmSaRun(ctx, conn, args, "unmap_vol", fields)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(""), nil

	default:
		return Result{}, errArg("ibm_sa_vol_map: state must be present or absent, got %q", state)
	}
}
