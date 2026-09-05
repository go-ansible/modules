package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDnsimpleInfo implements (a subset of) Ansible's
// `dnsimple_info` module: a read-only lookup of domains/records via
// DNSimple's own official `dnsimple` CLI — see dnsimple_common.go's own
// doc comment for the CLI substitution rationale and auth wiring.
//
// Args: api_key (string, required) — wired as DNSIMPLE_TOKEN; account_id
// (string, required by real dnsimple_info.py) — accepted for
// argument-shape compatibility but NOT wired through as a `--account`
// flag (see dnsimple_common.go's own doc comment on why: this port
// always lets `dnsimple` resolve its own already-configured account);
// name (string, optional) — a domain name; when omitted, every domain
// on the resolved account is returned (`dnsimple_domain_info`); record
// (string, optional, requires name) — when given alongside name, only
// records whose own name exactly equals record are returned
// (`dnsimple_record_info`, matching real dnsimple_info.py's own
// server-side `?name=<record>` exact-match query parameter exactly:
// `dnsimple records list <name> --name <record> --json`); name alone
// (no record) returns every record for that domain
// (`dnsimple_records_info`). sandbox (bool, default false).
//
// This module never changes anything (Changed is always false), and
// never fails cleanly on "nothing found" — an empty result list is a
// legitimate, successful answer, matching real dnsimple_info.py's own
// behavior.
func moduleDnsimpleInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := dnsimpleRequireBinary(ctx, conn, "dnsimple_info"); !ok {
		return res, nil
	}
	token := argString(args, "api_key", "")
	sandbox := argBool(args, "sandbox", false)
	name := argString(args, "name", "")
	record := argString(args, "record", "")

	if name == "" {
		domains, res, err := dnsimpleRunList(ctx, conn, token, sandbox, "domains", "list")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("dnsimple_info: " + dnsimpleErrMsg(res)), nil
		}
		return Ok("").WithExtra("dnsimple_domain_info", domains), nil
	}

	if record != "" {
		records, res, err := dnsimpleRunList(ctx, conn, token, sandbox, "records", "list", name, "--name", record)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("dnsimple_info: " + dnsimpleErrMsg(res)), nil
		}
		return Ok("").WithExtra("dnsimple_record_info", records), nil
	}

	records, res, err := dnsimpleRunList(ctx, conn, token, sandbox, "records", "list", name)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("dnsimple_info: " + dnsimpleErrMsg(res)), nil
	}
	return Ok("").WithExtra("dnsimple_records_info", records), nil
}
