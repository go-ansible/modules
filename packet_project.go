package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacketProject implements Ansible's `packet_project`
// (community.general) module: creates or deletes an Equinix Metal
// project — see packet_common.go's own doc comment for the `metal` CLI
// substitution shared by every packet_* module in this batch. Commands
// used: `project get` (list, when id is not given — confirmed real:
// metal-cli's own generated docs document `metal project get` with no
// `-i` as a list operation, mirroring `ssh-key get`'s own documented
// "retrieves the SSH keys of the current user" no-id behavior),
// `project create -n <name> [-O <org_id>] [-m <payment_method_id>]`,
// `project delete -i <id> -f` — every flag independently confirmed
// from metal-cli's own generated per-command docs during this batch's
// own research.
//
// Args: auth_token (wired as METAL_AUTH_TOKEN — see packet_common.go's
// own doc comment); custom_data (accepted for argument-shape
// compatibility, NOT wired — `metal project create` has no matching
// flag in its own confirmed flag list; real packet_project.py's own
// custom_data is a packet-python-SDK-only field with no metal-cli
// equivalent found); id (takes precedence for lookup/delete, matching
// real packet_project.py's own `matching_projects` logic — read from
// source before implementing); name (used for lookup when id is not
// given, and required to create); org_id (-> -O); payment_method (->
// -m — real packet_project.py's own arg is a payment method NAME,
// while metal-cli's own -O/-m flags are documented as taking a UUID;
// this port passes payment_method straight through as if it were
// already the UUID metal-cli expects, a documented shape mismatch a
// real playbook using a human-readable payment method name would hit);
// state (present|absent, default present).
//
// Extra["id"]/Extra["name"]: present whenever the project now exists.
func modulePacketProject(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := metalRequireBinary(ctx, conn, "packet_project"); !ok {
		return res, nil
	}
	authToken := argString(args, "auth_token", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("packet_project: state must be one of present, absent, got %q", state)
	}
	id := argString(args, "id", "")
	name := argString(args, "name", "")
	if id == "" && name == "" {
		return Result{}, errArg("packet_project: one of id, name is required")
	}

	var listResp map[string]any
	lres, err := metalRunJSON(ctx, conn, authToken, &listResp, "project", "get")
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return metalFail("packet_project", "listing projects", lres), nil
	}
	items := metalListArray(listResp)
	var match map[string]any
	var found, ambiguous bool
	if id != "" {
		match, found, ambiguous = metalFindByField(items, "id", id)
	} else {
		match, found, ambiguous = metalFindByField(items, "name", name)
	}
	if ambiguous {
		return Fail("packet_project: more than one project matches; execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("packet_project: already absent"), nil
		}
		pid := fmt.Sprint(match["id"])
		dres, err := metalRun(ctx, conn, authToken, "project", "delete", "-i", pid, "-f")
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return metalFail("packet_project", "deleting "+pid, dres), nil
		}
		return Changed("packet_project: "+pid+" deleted").WithExtra("id", pid), nil
	}

	if found {
		return Ok("packet_project: already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("name", fmt.Sprint(match["name"])), nil
	}
	if name == "" {
		return Fail("packet_project: name is required to create a project"), nil
	}

	argv := []string{"project", "create", "-n", name}
	if v := argString(args, "org_id", ""); v != "" {
		argv = append(argv, "-O", v)
	}
	if v := argString(args, "payment_method", ""); v != "" {
		argv = append(argv, "-m", v)
	}
	var created map[string]any
	cres, err := metalRunJSON(ctx, conn, authToken, &created, argv...)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return metalFail("packet_project", "creating "+name, cres), nil
	}
	r := Changed("packet_project: " + name + " created")
	if id, ok := created["id"]; ok {
		r = r.WithExtra("id", fmt.Sprint(id)).WithExtra("name", name)
	}
	return r, nil
}
