package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewaySecurityGroup implements Ansible's
// `scaleway_security_group` (community.general) module: creates/
// deletes a Scaleway instance security group, via `scw instance
// security-group create/list/delete` — see scaleway_common.go's own
// doc comment for why this port substitutes the `scw` CLI, and for the
// region->zone mapping (scwZone) used below.
//
// Args: state (present|absent, default present); organization/project
// (exactly one required, regardless of state — same
// mutually_exclusive+required_one_of shape as scaleway_ip.go); region
// (required); name (required); description; stateful (required bool);
// inbound_default_policy/outbound_default_policy (accept|drop; required
// when stateful=true, matching real AnsibleModule's own
// required_if=[["stateful", True, [...]]]); organization_default
// (bool).
//
// Fidelity note — no update path: real present_strategy only creates
// or no-ops on an exact name match; it never diffs/patches an existing
// security group's own attributes (description/stateful/policies/
// organization_default) against the requested values — verified
// directly against scaleway_security_group.py's own source (no PATCH
// call exists in present_strategy at all). This port matches that
// exactly: finding an existing group by name is always Changed=false,
// even if description/stateful/policies differ from what was
// requested.
//
// Deviation — organization_default -> project-default: real ansible's
// own `organization_default` argument maps to `scw`'s own
// `project-default=` CLI key, not `organization-default=` — verified
// against `scw instance security-group create`'s own documented
// argument set (cli.scaleway.com/instance/), which has no
// "organization-default" key at all. A genuine platform renaming
// (organization-level default superseded by project-level), the same
// kind of flag-name exception ipa_common.go's own doc comment already
// calls out for `ipa`.
//
// present: `scw instance security-group list zone=<zone> -o json`
// (unfiltered — matching real present_strategy's own unfiltered GET),
// confirmed exact match on "name" client-side. Found -> Changed=false.
// Not found -> `scw instance security-group create name=<name>
// [description=<description>] stateful=<bool>
// [inbound-default-policy=<policy>] [outbound-default-policy=<policy>]
// [project-default=<bool>] organization-id=/project-id=<org-or-project>
// zone=<zone>`, Changed=true.
//
// absent: not found -> Changed=false; found -> `scw instance
// security-group delete security-group-id=<id> zone=<zone>`,
// Changed=true.
//
// Extra["security_group"]: the current/created object, decoded directly
// from `scw`'s own JSON output (see scaleway_common.go's own "Output
// shape" caveat).
func moduleScalewaySecurityGroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_security_group"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	region, err := requireString(args, "region")
	if err != nil {
		return Result{}, err
	}
	zone, err := scwZone(region)
	if err != nil {
		return Result{}, err
	}
	org := argString(args, "organization", "")
	project := argString(args, "project", "")
	if (org == "") == (project == "") {
		return Result{}, errArg("scaleway_security_group: exactly one of organization or project must be specified")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_security_group: state must be present or absent, got %q", state)
	}
	if _, ok := args["stateful"]; !ok {
		return Result{}, errArg("scaleway_security_group: stateful is required")
	}
	stateful := argBool(args, "stateful", false)
	inbound := argString(args, "inbound_default_policy", "")
	outbound := argString(args, "outbound_default_policy", "")
	if stateful && (inbound == "" || outbound == "") {
		return Result{}, errArg("scaleway_security_group: inbound_default_policy and outbound_default_policy are required when stateful=true")
	}
	description := argString(args, "description", "")
	_, hasOrgDefault := args["organization_default"]
	orgDefault := argBool(args, "organization_default", false)

	current, found, err := scwFindByName(ctx, conn, name, "instance", "security-group", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !found {
			return Ok(""), nil
		}
		id := scwSGStr(current, "id")
		delRes, err := scwRun(ctx, conn, "instance", "security-group", "delete", "security-group-id="+id, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_security_group: failed to delete security group " + name + ": " + scwErrMsg(delRes)), nil
		}
		return Changed(""), nil
	}

	if found {
		res := Result{Changed: false}
		return res.WithExtra("security_group", current), nil
	}

	createArgs := []string{"instance", "security-group", "create", "name=" + name}
	if description != "" {
		createArgs = append(createArgs, "description="+description)
	}
	createArgs = append(createArgs, "stateful="+boolStr(stateful))
	if inbound != "" {
		createArgs = append(createArgs, "inbound-default-policy="+inbound)
	}
	if outbound != "" {
		createArgs = append(createArgs, "outbound-default-policy="+outbound)
	}
	if hasOrgDefault {
		createArgs = append(createArgs, "project-default="+boolStr(orgDefault))
	}
	if org != "" {
		createArgs = append(createArgs, "organization-id="+org)
	} else {
		createArgs = append(createArgs, "project-id="+project)
	}
	createArgs = append(createArgs, "zone="+zone)
	createRes, err := scwRunJSON(ctx, conn, createArgs...)
	if err != nil {
		return Result{}, err
	}
	if createRes.RC != 0 {
		return Fail("scaleway_security_group: failed to create security group " + name + ": " + scwErrMsg(createRes)), nil
	}
	var created map[string]any
	if derr := scwDecode(createRes.Stdout, &created); derr != nil {
		return Result{}, derr
	}
	return Changed("").WithExtra("security_group", created), nil
}

func scwSGStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// boolStr renders a Go bool as scw's own "true"/"false" CLI literal —
// reusing the identical helper already defined package-wide in
// nomad_token.go (same name, same behavior).
