package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIbmSaDomain implements Ansible's `ibm_sa_domain` module: creates
// or deletes a storage domain on an IBM Spectrum Accelerate Family
// system (XIV / Spectrum Accelerate / FlashSystem A9000), via `xcli`
// (see ibm_sa_common.go's own doc comment for the xcli/pyxcli
// substitution and connection details shared by every ibm_sa_* module in
// this batch).
//
// Args: domain (required) — the domain name; state (present|absent,
// default present); ldap_id, size, hard_capacity, soft_capacity,
// max_cgs, max_dms, max_mirrors, max_pools, max_volumes, perf_class —
// all optional, forwarded verbatim as `domain_create`'s own xcli
// key=value fields when given (matching real ibm_sa_domain.py's own
// argument_spec exactly — every one of these fields is also a
// documented `domain_create` xcli parameter, not this port's own
// invention). username/password/endpoints are the array connection
// arguments (see ibm_sa_common.go).
//
// Idempotency matches real ibm_sa_domain.py exactly: `domain_list
// domain=<name>` decides existence; state=present creates only if
// absent, state=absent deletes only if present — no per-field diff/update
// (real ibm_sa_domain.py itself has none either: a domain_create with
// mismatched attributes against an already-existing domain of the same
// name is left alone, same as here).
func moduleIbmSaDomain(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	domain, err := requireString(args, "domain")
	if err != nil {
		return Result{}, err
	}
	if res, ok := ibmSaRequireBinary(ctx, conn, "ibm_sa_domain", args); !ok {
		return res, nil
	}
	state := argString(args, "state", "present")

	rows, ok, err := ibmSaList(ctx, conn, args, "domain_list", map[string]string{"domain": domain})
	if err != nil {
		return Result{}, err
	}
	exists := ok && len(rows) > 0

	switch state {
	case "present":
		if exists {
			return Ok(domainMsg(domain, "state unchanged.")), nil
		}
		fields := ibmSaFieldsFromArgs(args, "domain", "size", "max_dms", "max_cgs", "ldap_id", "max_mirrors",
			"max_pools", "max_volumes", "perf_class", "hard_capacity", "soft_capacity")
		res, err := ibmSaRun(ctx, conn, args, "domain_create", fields)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(domainMsg(domain, "created successfully.")), nil

	case "absent":
		if !exists {
			return Ok(domainMsg(domain, "state unchanged.")), nil
		}
		res, err := ibmSaRun(ctx, conn, args, "domain_delete", map[string]string{"domain": domain})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(ibmSaErrMsg(res)), nil
		}
		return Changed(domainMsg(domain, "deleted successfully.")), nil

	default:
		return Result{}, errArg("ibm_sa_domain: state must be present or absent, got %q", state)
	}
}

func domainMsg(domain, suffix string) string {
	return "Domain '" + domain + "' " + suffix
}
