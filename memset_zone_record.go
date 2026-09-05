package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMemsetZoneRecord implements Ansible's `memset_zone_record`
// module via Memset's own official `ma-shell` — see memset_common.go's
// own doc comment for the CLI-substitution rationale, ma-shell's own
// invocation syntax, and its verified quirks — including the boolean-
// false gap this module's own `relative` argument runs directly into
// (see below).
//
// Args: api_key (string, required, unavoidably on argv — see
// memset_common.go's own doc comment); zone (string, required); type
// (A|AAAA|CNAME|MX|NS|SRV|TXT, required); address (string, required,
// max 250 chars — real module's own aliases ip/data are not accepted
// here, matching this port's own single-resolved-argument-name
// convention); record (string, default "", max 63 chars); ttl (int,
// default 0, one of msValidTTL's own values); priority (int, default 0,
// 0-999 inclusive); relative (bool, default false) — only meaningful for
// CNAME/MX/NS/SRV.
//
// # relative=true works, relative=false is INDISTINGUISHABLE FROM
// # OMITTED — a direct consequence of ma-shell's own verified boolean bug
//
// Per memset_common.go's own doc comment, ma-shell's `(boolean)` cast
// cannot express `false` at all. Since `relative`'s own real API default
// is already `false`, an explicit `relative: false` in a playbook is
// simply never sent as a parameter at all (identical to leaving it
// unset) — behaviorally correct because it matches the server's own
// default, but this port cannot distinguish "explicitly false" from
// "omitted" when constructing the request, unlike real
// memset_zone_record.py (which always sends every field explicitly,
// including `relative: false`, since its own JSON/form-encoded transport
// has no such limitation). relative=true IS sent (ma-shell's own
// `bool("true")` correctly yields True).
//
// # RPC methods — verified directly in real memset_zone_record.py's own
// # source
//
// dns.zone_list (resolve `zone`'s own id); dns.zone_record_list (no
// params — like zone_domain_list, this returns EVERY record across
// every zone; real memset_zone_record.py itself has the identical
// "we can't limit records by zone" comment and always fetches the full
// list); dns.zone_record_create (zone_id, type, record, address, ttl,
// priority, relative); dns.zone_record_update (id, plus the same
// fields — real memset_zone_record.py merges the EXISTING record dict
// with the new field values before sending update, so unspecified
// fields are preserved server-side; this port sends the same full field
// set every time instead, since ma-shell has no partial-update shorthand
// and every field here already has a well-defined value, explicit or
// defaulted); dns.zone_record_delete (id).
//
// # Selection and idempotency — mirrors real memset_zone_record.py's own
// # create_zone_record/delete_zone_record exactly
//
// A record is matched by (zone_id, record, type) — real
// memset_zone_record.py's own exact filter — never by address/ttl/
// priority/relative, which are the fields a change can target. Zero
// matches -> dns.zone_record_create (state=present) or a no-op
// (state=absent). One or more matches (Memset itself does not enforce
// record-name+type uniqueness) -> state=present compares every
// candidate's own full field set against the desired one; an EXACT match
// is a no-op, otherwise dns.zone_record_update is issued for that
// candidate (matching real memset_zone_record.py's own single-record
// "if zone_record == new_record: nothing to do" / else-update loop,
// which only ever acts on ONE matching record per invocation even when
// several exist — a real module quirk this port reproduces rather than
// smooths over). state=absent deletes every matching record (matching
// real memset_zone_record.py's own delete loop, which iterates all
// matches).
func moduleMemsetZoneRecord(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	apiKey, err := requireString(args, "api_key")
	if err != nil {
		return Result{}, errArg("memset_zone_record: %v", err)
	}
	zoneName, err := requireString(args, "zone")
	if err != nil {
		return Result{}, errArg("memset_zone_record: %v", err)
	}
	recordType := argString(args, "type", "")
	switch recordType {
	case "A", "AAAA", "CNAME", "MX", "NS", "SRV", "TXT":
	default:
		return Result{}, errArg("memset_zone_record: type must be one of A, AAAA, CNAME, MX, NS, SRV, TXT, got %q", recordType)
	}
	address, err := requireString(args, "address")
	if err != nil {
		return Result{}, errArg("memset_zone_record: %v", err)
	}
	record := argString(args, "record", "")
	ttl := argInt(args, "ttl", 0)
	if !msValidTTL(ttl) {
		return Result{}, errArg("memset_zone_record: invalid ttl %d", ttl)
	}
	priority := argInt(args, "priority", 0)
	relative := argBool(args, "relative", false)
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("memset_zone_record: state must be present or absent, got %q", state)
	}

	if priority < 0 || priority > 999 {
		return Result{}, errArg("memset_zone_record: priority must be in the range 0-999 (inclusive)")
	}
	if len(address) > 250 {
		return Result{}, errArg("memset_zone_record: address must be less than 250 characters in length")
	}
	if len(record) > 63 {
		return Result{}, errArg("memset_zone_record: record must be less than 63 characters in length")
	}
	if relative {
		switch recordType {
		case "CNAME", "MX", "NS", "SRV":
		default:
			return Result{}, errArg("memset_zone_record: relative is only valid for CNAME, MX, NS and SRV record types")
		}
	}

	if res, ok := msRequireBinary(ctx, conn, "memset_zone_record"); !ok {
		return res, nil
	}

	zones, problem, err := msCall(ctx, conn, apiKey, "dns.zone_list", nil)
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone_record: dns.zone_list: %s", problem)), nil
	}
	var zoneMatches []map[string]any
	for _, z := range msArray(zones) {
		if fmt.Sprint(z["nickname"]) == zoneName {
			zoneMatches = append(zoneMatches, z)
		}
	}
	if len(zoneMatches) == 0 {
		return Fail(fmt.Sprintf("memset_zone_record: DNS zone %s does not exist.", zoneName)), nil
	}
	if len(zoneMatches) > 1 {
		return Fail(fmt.Sprintf("memset_zone_record: %s matches multiple zones.", zoneName)), nil
	}
	zoneID := fmt.Sprint(zoneMatches[0]["id"])

	allRecords, problem, err := msCall(ctx, conn, apiKey, "dns.zone_record_list", nil)
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone_record: dns.zone_record_list: %s", problem)), nil
	}
	var matches []map[string]any
	for _, r := range msArray(allRecords) {
		if fmt.Sprint(r["zone_id"]) == zoneID && fmt.Sprint(r["record"]) == record && fmt.Sprint(r["type"]) == recordType {
			matches = append(matches, r)
		}
	}

	recordParams := func(extra ...msParam) []msParam {
		p := []msParam{
			msStr("zone_id", zoneID),
			msStr("type", recordType),
			msStr("record", record),
			msStr("address", address),
			msInt("ttl", ttl),
			msInt("priority", priority),
		}
		if relative {
			p = append(p, msBoolTrue("relative"))
		}
		return append(p, extra...)
	}

	if state == "absent" {
		if len(matches) == 0 {
			return Ok(fmt.Sprintf("no matching record for %s in %s", record, zoneName)), nil
		}
		for _, m := range matches {
			_, problem, err := msCall(ctx, conn, apiKey, "dns.zone_record_delete",
				[]msParam{msStr("id", fmt.Sprint(m["id"]))})
			if err != nil {
				return Result{}, err
			}
			if problem != "" {
				return Fail(fmt.Sprintf("memset_zone_record: dns.zone_record_delete: %s", problem)), nil
			}
		}
		return Changed(fmt.Sprintf("deleted %d matching record(s)", len(matches))).WithExtra("memset_api", matches[0]), nil
	}

	// state == present
	if len(matches) == 0 {
		result, problem, err := msCall(ctx, conn, apiKey, "dns.zone_record_create", recordParams())
		if err != nil {
			return Result{}, err
		}
		if problem != "" {
			return Fail(fmt.Sprintf("memset_zone_record: dns.zone_record_create: %s", problem)), nil
		}
		return Changed(fmt.Sprintf("created %s record for %s", recordType, zoneName)).WithExtra("memset_api", result), nil
	}

	// Real memset_zone_record.py only ever acts on the FIRST matching
	// record (see this module's own doc comment).
	existing := matches[0]
	if memsetRecordMatches(existing, zoneID, recordType, record, address, ttl, priority, relative) {
		return Ok("record already matches the desired state").WithExtra("memset_api", existing), nil
	}
	result, problem, err := msCall(ctx, conn, apiKey, "dns.zone_record_update",
		recordParams(msStr("id", fmt.Sprint(existing["id"]))))
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone_record: dns.zone_record_update: %s", problem)), nil
	}
	return Changed(fmt.Sprintf("updated %s record for %s", recordType, zoneName)).WithExtra("memset_api", result), nil
}

// memsetRecordMatches reports whether an existing record (as decoded
// from dns.zone_record_list's own JSON) already has every field this
// module would otherwise send via dns.zone_record_update — mirroring
// real memset_zone_record.py's own `zone_record == new_record` full-
// dict comparison.
func memsetRecordMatches(existing map[string]any, zoneID, recordType, record, address string, ttl, priority int, relative bool) bool {
	if fmt.Sprint(existing["zone_id"]) != zoneID {
		return false
	}
	if fmt.Sprint(existing["type"]) != recordType {
		return false
	}
	if fmt.Sprint(existing["record"]) != record {
		return false
	}
	if fmt.Sprint(existing["address"]) != address {
		return false
	}
	if argInt(existing, "ttl", -1) != ttl {
		return false
	}
	if argInt(existing, "priority", -1) != priority {
		return false
	}
	return argBool(existing, "relative", false) == relative
}
