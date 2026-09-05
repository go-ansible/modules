package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIbmSaHostPorts implements Ansible's `ibm_sa_host_ports` module:
// adds or removes one FC/iSCSI port from a host object on an IBM
// Spectrum Accelerate Family system, via `xcli` (see ibm_sa_common.go's
// own doc comment).
//
// Args: host (required); state (present|absent, default present);
// iscsi_name, fcaddress, num_of_visible_targets — optional, forwarded as
// `host_add_port`/`host_remove_port`'s own xcli fields (matching real
// ibm_sa_host_ports.py's own argument_spec).
//
// Idempotency matches real ibm_sa_host_ports.py exactly: `host_list_ports
// host=<name>` lists the host's current ports (this port's own
// ibmSaList reads xcli's `port_name` CSV column, matching real
// `.as_list`'s own `port.get("port_name")` extraction); the port is
// considered already present if either iscsi_name or fcaddress (whichever
// was given) matches an existing port_name exactly.
func moduleIbmSaHostPorts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	host, err := requireString(args, "host")
	if err != nil {
		return Result{}, err
	}
	if res, ok := ibmSaRequireBinary(ctx, conn, "ibm_sa_host_ports", args); !ok {
		return res, nil
	}
	state := argString(args, "state", "present")
	iscsiName := argString(args, "iscsi_name", "")
	fcAddress := argString(args, "fcaddress", "")

	rows, _, err := ibmSaList(ctx, conn, args, "host_list_ports", map[string]string{"host": host})
	if err != nil {
		return Result{}, err
	}
	portExists := false
	for _, row := range rows {
		name := row["port_name"]
		if (iscsiName != "" && name == iscsiName) || (fcAddress != "" && name == fcAddress) {
			portExists = true
			break
		}
	}

	fields := ibmSaFieldsFromArgs(args, "host", "iscsi_name", "fcaddress", "num_of_visible_targets")

	switch state {
	case "present":
		if portExists {
			return Ok(""), nil
		}
		res, err := ibmSaRun(ctx, conn, args, "host_add_port", fields)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(""), nil

	case "absent":
		if !portExists {
			return Ok(""), nil
		}
		res, err := ibmSaRun(ctx, conn, args, "host_remove_port", fields)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(""), nil

	default:
		return Result{}, errArg("ibm_sa_host_ports: state must be present or absent, got %q", state)
	}
}
