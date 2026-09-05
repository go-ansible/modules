package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMemsetZone implements Ansible's `memset_zone` module via
// Memset's own official `ma-shell` — see memset_common.go's own doc
// comment for the CLI-substitution rationale, ma-shell's own invocation
// syntax, and its verified quirks.
//
// Args: api_key (string, required, unavoidably on argv — see
// memset_common.go's own doc comment); name (string, required — real
// module's own alias `nickname` is not accepted here, matching this
// port's own convention of only ever taking one already-resolved
// argument name, e.g. acl.go's own doc comment); state (present|absent,
// required); ttl (int, default 0, one of the exact values
// msValidTTL accepts — matching every real memset_zone*.py module's own
// `choices`); force (bool, default false) — required to delete a zone
// that still contains domains/records.
//
// # RPC methods — verified directly in real memset_zone.py's own source
//
// dns.zone_list (no params) enumerates every zone; dns.zone_create
// (nickname, ttl) creates one; dns.zone_update (id, ttl) updates an
// existing zone's TTL (the only field real memset_zone.py itself ever
// updates in place); dns.zone_info (id) fetches full detail for the
// final return value; dns.zone_delete (id) deletes one.
//
// # Selection and idempotency — mirrors real memset_zone.py's own
// # create_zone/delete_zone exactly
//
// A zone is looked up by its own `nickname` field (Memset's zone list
// has no separate "name" concept) via dns.zone_list. Real
// memset_zone.py's own zone-name UNIQUENESS enforcement differs between
// create and delete: create_zone matches the FIRST zone with that
// nickname (Memset's own API allows non-unique nicknames — the real
// module does not attempt to disambiguate for create at all); delete_zone
// explicitly counts matches and Fails ("Unable to delete zone as
// multiple zones with the same name exist.") if more than one exists.
// This port replicates both asymmetric behaviors exactly rather than
// applying a single, more consistent rule of its own.
//
// state=present: not found -> dns.zone_create; found with a different
// ttl -> dns.zone_update; found with the same ttl -> no-op. Either way
// the final dns.zone_info for the (possibly just-created) zone is
// returned as Extra["memset_api"], matching real memset_zone.py's own
// final re-fetch.
//
// state=absent: not found -> no-op; found exactly once with domains=0
// and records=0 (or force=true) -> dns.zone_delete; found exactly once
// with domains/records present and force=false -> Fail (a well-formed,
// expected refusal, matching real memset_zone.py's own
// module.fail_json for this exact case — Result{Failed:true}, not a Go
// error); found more than once -> Fail ("multiple zones... execution
// aborted"-equivalent, matching real memset_zone.py's own message
// verbatim).
func moduleMemsetZone(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	apiKey, err := requireString(args, "api_key")
	if err != nil {
		return Result{}, errArg("memset_zone: %v", err)
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, errArg("memset_zone: %v", err)
	}
	state := argString(args, "state", "")
	if state != "present" && state != "absent" {
		return Result{}, errArg("memset_zone: state must be present or absent, got %q", state)
	}
	if len(name) > 250 {
		return Result{}, errArg("memset_zone: name must be less than 250 characters in length")
	}
	ttl := argInt(args, "ttl", 0)
	if !msValidTTL(ttl) {
		return Result{}, errArg("memset_zone: invalid ttl %d", ttl)
	}
	force := argBool(args, "force", false)

	if res, ok := msRequireBinary(ctx, conn, "memset_zone"); !ok {
		return res, nil
	}

	zones, problem, err := msCall(ctx, conn, apiKey, "dns.zone_list", nil)
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone: dns.zone_list: %s", problem)), nil
	}
	zoneList := msArray(zones)

	var matches []map[string]any
	for _, z := range zoneList {
		if fmt.Sprint(z["nickname"]) == name {
			matches = append(matches, z)
		}
	}

	if state == "present" {
		return memsetZonePresent(ctx, conn, apiKey, name, ttl, matches)
	}
	return memsetZoneAbsent(ctx, conn, apiKey, name, force, matches)
}

func memsetZonePresent(ctx context.Context, conn remoteexec.Connection, apiKey, name string, ttl int, matches []map[string]any) (Result, error) {
	changed := false
	var zoneID string

	if len(matches) == 0 {
		result, problem, err := msCall(ctx, conn, apiKey, "dns.zone_create",
			[]msParam{msStr("nickname", name), msInt("ttl", ttl)})
		if err != nil {
			return Result{}, err
		}
		if problem != "" {
			return Fail(fmt.Sprintf("memset_zone: dns.zone_create: %s", problem)), nil
		}
		changed = true
		created := msObject(result)
		zoneID = fmt.Sprint(created["id"])
	} else {
		zone := matches[0]
		zoneID = fmt.Sprint(zone["id"])
		curTTL := argInt(zone, "ttl", -1)
		if curTTL != ttl {
			_, problem, err := msCall(ctx, conn, apiKey, "dns.zone_update",
				[]msParam{msStr("id", zoneID), msInt("ttl", ttl)})
			if err != nil {
				return Result{}, err
			}
			if problem != "" {
				return Fail(fmt.Sprintf("memset_zone: dns.zone_update: %s", problem)), nil
			}
			changed = true
		}
	}

	info, problem, err := msCall(ctx, conn, apiKey, "dns.zone_info", []msParam{msStr("id", zoneID)})
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone: dns.zone_info: %s", problem)), nil
	}

	res := Ok(fmt.Sprintf("zone %s already up to date", name))
	if changed {
		res = Changed(fmt.Sprintf("zone %s created or updated", name))
	}
	return res.WithExtra("memset_api", msObject(info)), nil
}

func memsetZoneAbsent(ctx context.Context, conn remoteexec.Connection, apiKey, name string, force bool, matches []map[string]any) (Result, error) {
	if len(matches) == 0 {
		return Ok(fmt.Sprintf("zone %s does not exist", name)), nil
	}
	if len(matches) > 1 {
		return Fail("memset_zone: Unable to delete zone as multiple zones with the same name exist."), nil
	}
	zone := matches[0]
	domainCount := argListLen(zone["domains"])
	recordCount := argListLen(zone["records"])
	if (domainCount > 0 || recordCount > 0) && !force {
		return Fail("memset_zone: Zone contains domains or records and force was not used."), nil
	}

	zoneID := fmt.Sprint(zone["id"])
	result, problem, err := msCall(ctx, conn, apiKey, "dns.zone_delete", []msParam{msStr("id", zoneID)})
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone: dns.zone_delete: %s", problem)), nil
	}
	return Changed(fmt.Sprintf("zone %s deleted", name)).WithExtra("memset_api", result), nil
}

// argListLen returns len(v) when v is a JSON array (as decoded into
// []any), else 0.
func argListLen(v any) int {
	if arr, ok := v.([]any); ok {
		return len(arr)
	}
	return 0
}
