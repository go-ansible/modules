package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// hwcSecurityGroupRuleOptionalFields lists this module's own optional
// string args that map 1:1 onto a same-named field on the real
// security-group-rule resource (verified against real
// hwc_vpc_security_group_rule.py's own argument_spec, read before
// implementing) — used both to build the create body and as extra
// selector fields at lookup time (a rule has no natural short "name",
// so this port leans harder on this batch's generic selector matching
// than most other hwc_* modules do).
var hwcSecurityGroupRuleOptionalFields = []string{
	"description", "ethertype", "protocol", "remote_group_id", "remote_ip_prefix",
}

// moduleHwcVpcSecurityGroupRule implements Ansible's
// `hwc_vpc_security_group_rule` (community.general) module: creates or
// deletes one rule inside a Huawei Cloud VPC security group — see
// hwc_common.go's own doc comment for the KooCLI substitution shared
// by every hwc_* module in this batch. Operation IDs
// (CreateSecurityGroupRule/ShowSecurityGroupRule/
// DeleteSecurityGroupRule/ListSecurityGroupRules, KooCLI service code
// "VPC") are DERIVED from real hwc_vpc_security_group_rule.py's own
// REST path ("security-group-rules/{id}", read before implementing),
// following hwc_common.go's own confirmed PascalCase(Verb+Resource)
// convention.
//
// Args: direction, security_group_id (required); description,
// ethertype, port_range_max, port_range_min, protocol,
// remote_group_id, remote_ip_prefix (all optional); id (takes
// precedence for lookup); region; state (present|absent, default
// present).
//
// Lookup: id given -> ShowSecurityGroupRule; else
// ListSecurityGroupRules filtered client-side by security_group_id +
// direction + every optional field actually given a value — this
// rule has no independent name, so every specified field narrows the
// match, matching the spirit of every other hwc_* module's own
// "execution is aborted" ambiguity handling. state=present on an
// already-found rule is always a no-op (see hwc_common.go's own doc
// comment on this batch's uniform no-update simplification —
// individual security group rules are commonly treated as immutable
// by cloud APIs in general, so this is a low-risk case for it).
//
// Extra["id"]/Extra["security_group_rule"]: as returned by KooCLI,
// present whenever the rule now exists.
func moduleHwcVpcSecurityGroupRule(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_vpc_security_group_rule"); !ok {
		return res, nil
	}
	direction, err := requireString(args, "direction")
	if err != nil {
		return Result{}, err
	}
	sgID, err := requireString(args, "security_group_id")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_vpc_security_group_rule: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)

	selector := map[string]string{"security_group_id": sgID, "direction": direction}
	for _, f := range hwcSecurityGroupRuleOptionalFields {
		if v := argString(args, f, ""); v != "" {
			selector[f] = v
		}
	}
	if _, ok := args["port_range_max"]; ok {
		selector["port_range_max"] = fmt.Sprint(argInt(args, "port_range_max", 0))
	}
	if _, ok := args["port_range_min"]; ok {
		selector["port_range_min"] = fmt.Sprint(argInt(args, "port_range_min", 0))
	}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowSecurityGroupRule", "ListSecurityGroupRules",
		"security_group_rule_id", argString(args, "id", ""), selector, region)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail("hwc_vpc_security_group_rule: more than one rule matches the given selector; execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_vpc_security_group_rule: already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := hcloudRun(ctx, conn, "VPC", "DeleteSecurityGroupRule", mergeParams(map[string]string{"security_group_rule_id": id}, region))
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_vpc_security_group_rule", "deleting rule "+id, dres), nil
		}
		return Changed("hwc_vpc_security_group_rule: "+id+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_vpc_security_group_rule: already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("security_group_rule", match), nil
	}

	cparams := map[string]string{
		"security_group_rule.direction": direction, "security_group_rule.security_group_id": sgID,
	}
	for _, f := range hwcSecurityGroupRuleOptionalFields {
		if v := argString(args, f, ""); v != "" {
			cparams["security_group_rule."+f] = v
		}
	}
	if _, ok := args["port_range_max"]; ok {
		cparams["security_group_rule.port_range_max"] = fmt.Sprint(argInt(args, "port_range_max", 0))
	}
	if _, ok := args["port_range_min"]; ok {
		cparams["security_group_rule.port_range_min"] = fmt.Sprint(argInt(args, "port_range_min", 0))
	}
	cparams = mergeParams(cparams, region)

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreateSecurityGroupRule", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_vpc_security_group_rule", "creating rule", cres), nil
	}
	rule, _ := created["security_group_rule"].(map[string]any)
	r := Changed("hwc_vpc_security_group_rule: created")
	if rule != nil {
		r = r.WithExtra("id", fmt.Sprint(rule["id"])).WithExtra("security_group_rule", rule)
	}
	return r, nil
}
