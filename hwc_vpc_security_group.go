package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcVpcSecurityGroup implements Ansible's
// `hwc_vpc_security_group` (community.general) module: creates or
// deletes a Huawei Cloud VPC security group — see hwc_common.go's own
// doc comment for the KooCLI substitution shared by every hwc_*
// module in this batch. Operation IDs
// (CreateSecurityGroup/ShowSecurityGroup/DeleteSecurityGroup/
// ListSecurityGroups, KooCLI service code "VPC") are DERIVED from real
// hwc_vpc_security_group.py's own REST path
// ("security-groups/{id}", read before implementing), following
// hwc_common.go's own confirmed PascalCase(Verb+Resource) convention.
//
// Args: name (required); enterprise_project_id, vpc_id (both
// optional); id (takes precedence for lookup); region; state
// (present|absent, default present).
//
// Lookup: id given -> ShowSecurityGroup; else ListSecurityGroups
// filtered client-side by whichever of name/enterprise_project_id/
// vpc_id were actually given — real hwc_vpc_security_group.py's own
// NOTES document exactly this set ("`name', `enterprise_project_id'
// and `vpc_id' are used for security group selection"). Real
// hwc_vpc_security_group.py's own NOTES also confirm this specific
// resource never supports update ("No parameter support updating...")
// — matching hwc_common.go's own uniform no-update simplification for
// every module in this batch.
//
// Extra["id"]/Extra["security_group"]: as returned by KooCLI, present
// whenever the security group now exists.
func moduleHwcVpcSecurityGroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_vpc_security_group"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_vpc_security_group: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)

	selector := map[string]string{"name": name}
	if v := argString(args, "vpc_id", ""); v != "" {
		selector["vpc_id"] = v
	}
	if v := argString(args, "enterprise_project_id", ""); v != "" {
		selector["enterprise_project_id"] = v
	}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "VPC", "ShowSecurityGroup", "ListSecurityGroups",
		"security_group_id", argString(args, "id", ""), selector, region)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail("hwc_vpc_security_group: more than one security group matches the given selector; execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_vpc_security_group: " + name + " already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := hcloudRun(ctx, conn, "VPC", "DeleteSecurityGroup", mergeParams(map[string]string{"security_group_id": id}, region))
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return hcloudFail("hwc_vpc_security_group", "deleting security group "+id, dres), nil
		}
		return Changed("hwc_vpc_security_group: "+name+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_vpc_security_group: "+name+" already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("security_group", match), nil
	}

	cparams := map[string]string{"security_group.name": name}
	if v := argString(args, "vpc_id", ""); v != "" {
		cparams["security_group.vpc_id"] = v
	}
	if v := argString(args, "enterprise_project_id", ""); v != "" {
		cparams["security_group.enterprise_project_id"] = v
	}
	cparams = mergeParams(cparams, region)

	var created map[string]any
	cres, err := hcloudRunJSON(ctx, conn, "VPC", "CreateSecurityGroup", cparams, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return hcloudFail("hwc_vpc_security_group", "creating security group "+name, cres), nil
	}
	sg, _ := created["security_group"].(map[string]any)
	r := Changed("hwc_vpc_security_group: " + name + " created")
	if sg != nil {
		r = r.WithExtra("id", fmt.Sprint(sg["id"])).WithExtra("security_group", sg)
	}
	return r, nil
}
