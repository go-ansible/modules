package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaDnsrecord implements (a subset of) Ansible's
// `ipa_dnsrecord` module: manages one DNS record (name + type) inside
// a FreeIPA-managed zone via the real `ipa` CLI's own
// `dnsrecord-add`/`dnsrecord-mod`/`dnsrecord-del`/`dnsrecord-show`
// subcommands. See ipa_common.go's own doc comment for this port's
// shared architecture and the connection-argument gap.
//
// Args: zone_name (string, required); record_name (string, required,
// aliased from name); record_type (A|AAAA|A6|CNAME|DNAME|MX|NS|PTR|
// SRV|TXT|SSHFP, default "A"); exactly one of record_value (string) or
// record_values (list of string) is required, matching real
// ipa_dnsrecord's own documented mutual-exclusivity/required-one
// constraint; record_ttl (int, optional) -> `--dnsttl`, best-effort
// only (see below); exclusive (bool, default true) — matches real
// ipa_dnsrecord's own four documented present/absent x true/false
// combinations exactly (see moduleIpaDnsrecord's own logic below);
// state (present|absent, default "present").
//
// Every record type's whole-value CLI flag follows the SAME verified
// "--<lowercase-type>record" pattern (--arecord, --aaaarecord,
// --cnamerecord, --mxrecord, --txtrecord, --srvrecord, --ptrrecord,
// --nsrecord all individually confirmed against FreeIPA's own API
// command reference; --a6record, --dnamerecord, --sshfprecord follow
// by the same uniform, unbroken pattern rather than being individually
// confirmed — flagged here per this batch's own honesty rule, not
// silently assumed equal-confidence to the eight verified ones), and
// the raw attribute name `dnsrecord-show --raw` reports current values
// under is the identical string (again verified for the eight, assumed
// for the other three by the same pattern).
//
// Semantics (matching real ipa_dnsrecord's own documented four-way
// present/absent x exclusive/non-exclusive matrix exactly):
//   - state=present, exclusive=true: the given value(s) REPLACE every
//     existing value of this type/name. Implemented as `dnsrecord-add`
//     (creates the name if it doesn't exist yet) when the record name
//     has no existing entry at all, or `dnsrecord-mod` with repeated
//     `--<type>record=` flags (a `-mod` replaces that specific
//     attribute's value set without touching other record types under
//     the same name) when it does.
//   - state=present, exclusive=false: the given value(s) are ADDED
//     alongside any existing values of this type/name — real
//     `dnsrecord-add` is itself additive when called against an
//     already-existing name (it does not require or imply the name is
//     new), so this port always uses `dnsrecord-add` here, and only
//     for the values not already present (an idempotency pre-check via
//     `dnsrecord-show --raw`).
//   - state=absent, exclusive=true: ALL existing values of this type/
//     name are removed, via `dnsrecord-mod --<type>record=` (the same
//     empty-value-clears-the-attribute idiom this port's ipaListDiff
//     already uses elsewhere), regardless of what record_value(s) was
//     given.
//   - state=absent, exclusive=false: only the given value(s) are
//     removed (values not currently present are silently skipped), via
//     `dnsrecord-del` with the matching `--<type>record=` flags.
//
// record_ttl is applied best-effort: it is included in whichever
// add/mod call this port ends up making for a value-set change, but a
// TTL-only change (same values, different record_ttl) is NOT detected
// or applied on its own — this port's idempotency check compares only
// the record's values, not its TTL, a deliberate narrowing given this
// batch's scope.
func moduleIpaDnsrecord(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	zoneName, err := requireString(args, "zone_name")
	if err != nil {
		return Result{}, err
	}
	recordName := argString(args, "record_name", argString(args, "name", ""))
	if recordName == "" {
		return Result{}, errArg("ipa_dnsrecord: record_name (or name) is required")
	}
	recordType := strings.ToUpper(argString(args, "record_type", "A"))
	switch recordType {
	case "A", "AAAA", "A6", "CNAME", "DNAME", "MX", "NS", "PTR", "SRV", "TXT", "SSHFP":
	default:
		return Result{}, errArg("ipa_dnsrecord: record_type must be one of A, AAAA, A6, CNAME, DNAME, MX, NS, PTR, SRV, TXT, SSHFP, got %q", recordType)
	}
	singleValue := argString(args, "record_value", "")
	listValues := argStringList(args, "record_values")
	if singleValue != "" && len(listValues) > 0 {
		return Result{}, errArg("ipa_dnsrecord: record_value and record_values are mutually exclusive")
	}
	if singleValue == "" && len(listValues) == 0 {
		return Result{}, errArg("ipa_dnsrecord: exactly one of record_value or record_values is required")
	}
	values := listValues
	if singleValue != "" {
		values = []string{singleValue}
	}
	exclusive := argBool(args, "exclusive", true)
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_dnsrecord: state must be present or absent, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_dnsrecord"); !ok {
		return res, nil
	}

	rawAttr := strings.ToLower(recordType) + "record"
	ttlFlag := ""
	if _, ok := args["record_ttl"]; ok {
		ttlFlag = "--dnsttl=" + argString(args, "record_ttl", "")
	}

	showRes, err := ipaRun(ctx, conn, "dnsrecord-show", zoneName, recordName, "--all", "--raw")
	if err != nil {
		return Result{}, err
	}
	present := showRes.RC == 0
	var currentVals []string
	if present {
		currentVals = ipaParseRaw(showRes.Stdout)[rawAttr]
	}

	label := recordName + "." + zoneName + " (" + recordType + ")"

	if state == "absent" {
		if exclusive {
			if len(currentVals) == 0 {
				return Ok(label + " already absent"), nil
			}
			res, err := ipaRun(ctx, conn, "dnsrecord-mod", zoneName, recordName, "--"+rawAttr+"=")
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_dnsrecord", "dnsrecord-mod (clear)", res), nil
			}
			return Changed(label + " all values removed"), nil
		}
		var toRemove []string
		curSet := map[string]bool{}
		for _, v := range currentVals {
			curSet[v] = true
		}
		for _, v := range values {
			if curSet[v] {
				toRemove = append(toRemove, v)
			}
		}
		if len(toRemove) == 0 {
			return Ok(label + " values already absent"), nil
		}
		flags := append([]string{"dnsrecord-del", zoneName, recordName}, ipaFlagRepeat(rawAttr, toRemove)...)
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_dnsrecord", "dnsrecord-del", res), nil
		}
		return Changed(label + " values removed"), nil
	}

	// state == "present"
	if exclusive {
		if stringSetEqual(values, currentVals) {
			return Ok(label + " already set"), nil
		}
		flags := ipaFlagRepeat(rawAttr, values)
		if ttlFlag != "" {
			flags = append(flags, ttlFlag)
		}
		var subcmd string
		if present {
			subcmd = "dnsrecord-mod"
		} else {
			subcmd = "dnsrecord-add"
		}
		res, err := ipaRun(ctx, conn, append([]string{subcmd, zoneName, recordName}, flags...)...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_dnsrecord", subcmd, res), nil
		}
		return Changed(label + " set"), nil
	}

	curSet := map[string]bool{}
	for _, v := range currentVals {
		curSet[v] = true
	}
	var toAdd []string
	for _, v := range values {
		if !curSet[v] {
			toAdd = append(toAdd, v)
		}
	}
	if len(toAdd) == 0 {
		return Ok(label + " values already present"), nil
	}
	flags := ipaFlagRepeat(rawAttr, toAdd)
	if ttlFlag != "" {
		flags = append(flags, ttlFlag)
	}
	res, err := ipaRun(ctx, conn, append([]string{"dnsrecord-add", zoneName, recordName}, flags...)...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return ipaFailedf("ipa_dnsrecord", "dnsrecord-add", res), nil
	}
	return Changed(label + " values added"), nil
}
