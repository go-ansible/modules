package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDnsimple implements (a subset of) Ansible's `dnsimple` module:
// manages domains and DNS records via DNSimple's own official `dnsimple`
// CLI — see dnsimple_common.go's own doc comment for the CLI
// substitution rationale, auth wiring (account_api_token ->
// DNSIMPLE_TOKEN, account_email a no-op), and JSON output shape every
// helper here relies on.
//
// Args: account_email (no effect — see dnsimple_common.go);
// account_api_token (string) — wired as DNSIMPLE_TOKEN; domain (string)
// — a domain name or numeric ID; when omitted, all domains are listed
// (unchanged); when given but record is omitted (not merely empty:
// see below) and record_ids is empty, the domain itself is
// created/deleted; record (string) — when given (including "" for an
// apex record — matching real dnsimple.py's own "if record is not
// None" distinction), a single DNS record is ensured; record_ids
// ([]string) — an alternative to record: ensures a specific set of
// already-created record IDs either all exist (state=present, a Fail
// if any are missing — this port cannot create a record from just an
// ID) or are all removed (state=absent); type (string, required with
// record); ttl (int, default 3600); value (string, required with
// record — the record's content); priority (int, optional); state
// (present|absent, default "present"); solo (bool, default false) —
// with record and state=present, deletes every other record sharing
// the same name+type; sandbox (bool, default false) — DNSimple's own
// sandbox environment.
//
// Idempotency for a named record matches real dnsimple.py's own
// technique exactly: `dnsimple records list <domain> --name <record>
// --json` (an exact-name server-side filter — dnsimple-cli's own
// documented `--name` flag, verified in zones_records.go), then a
// client-side match on type+content among the results — a record
// existing with the same name+type+content is "present"; ttl/priority
// differing from what's requested triggers an update rather than a
// no-op, matching real dnsimple.py's own `rr["ttl"] != ttl or
// rr["priority"] != priority` check.
func moduleDnsimple(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := dnsimpleRequireBinary(ctx, conn, "dnsimple"); !ok {
		return res, nil
	}
	token := argString(args, "account_api_token", "")
	sandbox := argBool(args, "sandbox", false)
	state := argString(args, "state", "present")
	ttl := argInt(args, "ttl", 3600)
	_, hasPriority := args["priority"]

	_, hasDomain := args["domain"]
	domain := argString(args, "domain", "")

	if !hasDomain || domain == "" {
		domains, res, err := dnsimpleRunList(ctx, conn, token, sandbox, "domains", "list")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("dnsimple: " + dnsimpleErrMsg(res)), nil
		}
		return Ok("").WithExtra("result", domains), nil
	}

	recordRaw, hasRecord := args["record"]
	_ = recordRaw
	recordIDs := argStringList(args, "record_ids")

	// Domain-only (no record, no record_ids): ensure the domain itself.
	if !hasRecord && len(recordIDs) == 0 {
		item, found, _, err := dnsimpleRunItem(ctx, conn, token, sandbox, "domains", "get", domain)
		if err != nil {
			return Result{}, err
		}
		switch state {
		case "absent":
			if !found {
				return Ok(domain + " already absent"), nil
			}
			if _, err := dnsimpleRun(ctx, conn, token, sandbox, "domains", "delete", domain, "--yes"); err != nil {
				return Result{}, err
			}
			return Changed(domain + " deleted"), nil
		default: // present
			if found {
				return Ok(domain+" already exists").WithExtra("result", item), nil
			}
			created, _, res, err := dnsimpleRunItem(ctx, conn, token, sandbox, "domains", "create", domain)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("dnsimple: " + dnsimpleErrMsg(res)), nil
			}
			return Changed(domain+" created").WithExtra("result", created), nil
		}
	}

	// record_ids form: ensure a set of already-created record IDs
	// either all exist or are all removed.
	if len(recordIDs) > 0 {
		current, res, err := dnsimpleRunList(ctx, conn, token, sandbox, "records", "list", domain)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("dnsimple: " + dnsimpleErrMsg(res)), nil
		}
		currentIDs := make(map[string]bool, len(current))
		for _, r := range current {
			currentIDs[fmt.Sprint(r["id"])] = true
		}
		if state == "absent" {
			var removed []string
			for _, id := range recordIDs {
				if currentIDs[id] {
					if _, err := dnsimpleRun(ctx, conn, token, sandbox, "records", "delete", domain, id, "--yes"); err != nil {
						return Result{}, err
					}
					removed = append(removed, id)
				}
			}
			if len(removed) == 0 {
				return Ok("no matching records"), nil
			}
			return Changed(fmt.Sprintf("removed %d record(s)", len(removed))), nil
		}
		var missing []string
		for _, id := range recordIDs {
			if !currentIDs[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			return Fail(fmt.Sprintf("dnsimple: missing the following records: %v", missing)), nil
		}
		return Ok("all records present"), nil
	}

	// Single named-record form.
	record := argString(args, "record", "")
	recordType, err := requireString(args, "type")
	if err != nil {
		return Fail("dnsimple: missing the record type"), nil
	}
	value := argString(args, "value", "")
	if state == "present" && value == "" {
		return Fail("dnsimple: missing the record value"), nil
	}
	solo := argBool(args, "solo", false)

	records, res, err := dnsimpleRunList(ctx, conn, token, sandbox, "records", "list", domain, "--name", record)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("dnsimple: " + dnsimpleErrMsg(res)), nil
	}

	var rr map[string]any
	for _, r := range records {
		if fmt.Sprint(r["name"]) == record && fmt.Sprint(r["type"]) == recordType && fmt.Sprint(r["content"]) == value {
			rr = r
			break
		}
	}

	changed := false
	if state == "present" {
		if solo {
			for _, r := range records {
				if rr != nil && fmt.Sprint(r["id"]) == fmt.Sprint(rr["id"]) {
					continue
				}
				if fmt.Sprint(r["name"]) == record && fmt.Sprint(r["type"]) == recordType {
					if _, err := dnsimpleRun(ctx, conn, token, sandbox, "records", "delete", domain, fmt.Sprint(r["id"]), "--yes"); err != nil {
						return Result{}, err
					}
					changed = true
				}
			}
		}
		if rr != nil {
			curTTL := argIntFromAny(rr["ttl"])
			needsUpdate := curTTL != ttl
			if hasPriority {
				needsUpdate = needsUpdate || argIntFromAny(rr["priority"]) != argInt(args, "priority", 0)
			}
			if needsUpdate {
				updateArgv := []string{"records", "update", domain, fmt.Sprint(rr["id"]), "--ttl", fmt.Sprint(ttl)}
				if hasPriority {
					updateArgv = append(updateArgv, "--priority", fmt.Sprint(argInt(args, "priority", 0)))
				}
				updated, _, ures, err := dnsimpleRunItem(ctx, conn, token, sandbox, updateArgv...)
				if err != nil {
					return Result{}, err
				}
				if ures.RC != 0 {
					return Fail("dnsimple: " + dnsimpleErrMsg(ures)), nil
				}
				return Changed(record+" updated").WithExtra("result", updated), nil
			}
			if changed {
				return Changed(record+" solo-cleaned").WithExtra("result", rr), nil
			}
			return Ok(record+" unchanged").WithExtra("result", rr), nil
		}
		createArgv := []string{"records", "create", domain, "--type", recordType, "--name", record, "--content", value, "--ttl", fmt.Sprint(ttl)}
		if hasPriority {
			createArgv = append(createArgv, "--priority", fmt.Sprint(argInt(args, "priority", 0)))
		}
		created, _, cres, err := dnsimpleRunItem(ctx, conn, token, sandbox, createArgv...)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("dnsimple: " + dnsimpleErrMsg(cres)), nil
		}
		return Changed(record+" created").WithExtra("result", created), nil
	}

	// state == "absent"
	if rr == nil {
		return Ok(record + " already absent"), nil
	}
	if _, err := dnsimpleRun(ctx, conn, token, sandbox, "records", "delete", domain, fmt.Sprint(rr["id"]), "--yes"); err != nil {
		return Result{}, err
	}
	return Changed(record + " removed"), nil
}

func argIntFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
