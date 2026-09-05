package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMemsetZoneDomain implements Ansible's `memset_zone_domain`
// module via Memset's own official `ma-shell` — see memset_common.go's
// own doc comment for the CLI-substitution rationale, ma-shell's own
// invocation syntax, and its verified quirks.
//
// Args: api_key (string, required, unavoidably on argv — see
// memset_common.go's own doc comment); domain (string, required — real
// module's own alias `name` is not accepted here, matching this port's
// own single-resolved-argument-name convention, e.g. acl.go's own doc
// comment); zone (string, required) — the containing zone's own
// nickname, which must already exist; state (present|absent, default
// present).
//
// # RPC methods — verified directly in real memset_zone_domain.py's own
// # source
//
// dns.zone_list (to resolve `zone`'s own id — Memset's zone_domain API
// has no separate "look up by zone nickname" call); dns.zone_domain_list
// (no params, returns every domain across every zone; real
// memset_zone_domain.py itself cannot filter this server-side either —
// it always fetches the FULL list and matches client-side);
// dns.zone_domain_create (domain, zone_id); dns.zone_domain_delete
// (domain); dns.zone_domain_info (domain) for the final return value.
//
// # Selection and idempotency — mirrors real memset_zone_domain.py's own
// # create_or_delete_domain exactly
//
// `zone` must resolve to EXACTLY one zone (0 or >1 matches both Fail,
// with real memset_zone_domain.py's own exact wording) before anything
// else is attempted. state=present: the domain already existing
// anywhere (Memset zone domains are unique across the whole account, not
// per-zone) is a no-op; otherwise dns.zone_domain_create. state=absent:
// not found is a no-op; found -> dns.zone_domain_delete (domains are
// always unique, so no ambiguity-count check is needed here, unlike
// memset_zone.go's own zone-delete path).
func moduleMemsetZoneDomain(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	apiKey, err := requireString(args, "api_key")
	if err != nil {
		return Result{}, errArg("memset_zone_domain: %v", err)
	}
	domain, err := requireString(args, "domain")
	if err != nil {
		return Result{}, errArg("memset_zone_domain: %v", err)
	}
	zoneName, err := requireString(args, "zone")
	if err != nil {
		return Result{}, errArg("memset_zone_domain: %v", err)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("memset_zone_domain: state must be present or absent, got %q", state)
	}
	if len(domain) > 250 {
		return Result{}, errArg("memset_zone_domain: domain must be less than 250 characters in length")
	}

	if res, ok := msRequireBinary(ctx, conn, "memset_zone_domain"); !ok {
		return res, nil
	}

	zones, problem, err := msCall(ctx, conn, apiKey, "dns.zone_list", nil)
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone_domain: dns.zone_list: %s", problem)), nil
	}
	var zoneMatches []map[string]any
	for _, z := range msArray(zones) {
		if fmt.Sprint(z["nickname"]) == zoneName {
			zoneMatches = append(zoneMatches, z)
		}
	}
	if len(zoneMatches) == 0 {
		return Fail(fmt.Sprintf("memset_zone_domain: DNS zone '%s' does not exist, cannot create domain.", zoneName)), nil
	}
	if len(zoneMatches) > 1 {
		return Fail(fmt.Sprintf("memset_zone_domain: %s matches multiple zones, cannot create domain.", zoneName)), nil
	}
	zoneID := fmt.Sprint(zoneMatches[0]["id"])

	domains, problem, err := msCall(ctx, conn, apiKey, "dns.zone_domain_list", nil)
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone_domain: dns.zone_domain_list: %s", problem)), nil
	}
	exists := false
	for _, d := range msArray(domains) {
		if fmt.Sprint(d["domain"]) == domain {
			exists = true
			break
		}
	}

	if state == "present" {
		if !exists {
			_, problem, err := msCall(ctx, conn, apiKey, "dns.zone_domain_create",
				[]msParam{msStr("domain", domain), msStr("zone_id", zoneID)})
			if err != nil {
				return Result{}, err
			}
			if problem != "" {
				return Fail(fmt.Sprintf("memset_zone_domain: dns.zone_domain_create: %s", problem)), nil
			}
		}
		info, problem, err := msCall(ctx, conn, apiKey, "dns.zone_domain_info", []msParam{msStr("domain", domain)})
		if err != nil {
			return Result{}, err
		}
		if problem != "" {
			return Fail(fmt.Sprintf("memset_zone_domain: dns.zone_domain_info: %s", problem)), nil
		}
		res := Ok(fmt.Sprintf("domain %s already exists", domain))
		if !exists {
			res = Changed(fmt.Sprintf("domain %s created", domain))
		}
		return res.WithExtra("memset_api", msObject(info)), nil
	}

	// state == absent
	if !exists {
		return Ok(fmt.Sprintf("domain %s does not exist", domain)), nil
	}
	result, problem, err := msCall(ctx, conn, apiKey, "dns.zone_domain_delete", []msParam{msStr("domain", domain)})
	if err != nil {
		return Result{}, err
	}
	if problem != "" {
		return Fail(fmt.Sprintf("memset_zone_domain: dns.zone_domain_delete: %s", problem)), nil
	}
	return Changed(fmt.Sprintf("domain %s deleted", domain)).WithExtra("memset_api", result), nil
}
