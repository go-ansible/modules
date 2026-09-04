package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayLB implements Ansible's `scaleway_lb` (community.general)
// module: creates/updates/deletes a Scaleway Load Balancer, via `scw lb
// lb create/list/get/update/delete` — see scaleway_common.go's own doc
// comment for why this port substitutes the `scw` CLI.
//
// Args: name (required); description (required); organization_id
// (required); region (required, choices fr-par/nl-ams/pl-waw — already
// region-shaped, validated by scwRegionArg); state (present|absent,
// default present); tags (list, default []); wait/wait_timeout/
// wait_sleep_time — accepted, no effect (see scaleway_common.go's own
// "wait/wait_timeout/wait_sleep_time" doc-comment section).
//
// Deviation — zone selection: real scaleway_lb's own `region` argument
// is genuinely region-wide (a Load Balancer can land in any zone of
// that region); `scw lb lb`'s own CLI commands take a concrete `zone=`
// argument instead (verified: fr-par-1, fr-par-2, nl-ams-1/2/3,
// pl-waw-1/2/3 — see scaleway_common.go's own doc comment). This port
// always targets that region's FIRST zone (region+"-1") — a real,
// disclosed narrowing, not a silent one.
//
// present: `scw lb lb list name=<name> zone=<zone> -o json`, confirmed
// exact-match on "name" (scwFindByName). Not found -> `scw lb lb create
// name=<name> description=<description> organization-id=<org>
// tags.N=<tag> zone=<zone>`, Changed=true. Found -> compare current
// name/description (MUTABLE_ATTRIBUTES in real lb_attributes_should_be_
// changed) against wished; real create_lb's own PUT-not-PATCH update
// requires BOTH — so a difference in either re-sends both via `scw lb
// lb update lb-id=<id> name=<name> description=<description>
// zone=<zone>`, Changed=true.
//
// absent: not found -> Changed=false; found -> `scw lb lb delete
// lb-id=<id> zone=<zone>`, Changed=true.
//
// Extra["lb"]: the created/current/updated LB object, decoded directly
// from `scw`'s own JSON output (see scaleway_common.go's own "Output
// shape" caveat).
func moduleScalewayLB(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_lb"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	description, err := requireString(args, "description")
	if err != nil {
		return Result{}, err
	}
	orgID, err := requireString(args, "organization_id")
	if err != nil {
		return Result{}, err
	}
	region, err := scwRegionArg(args, "scaleway_lb")
	if err != nil {
		return Result{}, err
	}
	zone := region + "-1"
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_lb: state must be present or absent, got %q", state)
	}
	tags := argStringList(args, "tags")

	current, found, err := scwFindByName(ctx, conn, name, "lb", "lb", "list", "name="+name, "zone="+zone)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !found {
			return Ok(""), nil
		}
		id := scwLBStr(current, "id")
		delRes, err := scwRun(ctx, conn, "lb", "lb", "delete", "lb-id="+id, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_lb: failed to delete load-balancer " + name + ": " + scwErrMsg(delRes)), nil
		}
		return Changed(""), nil
	}

	if !found {
		createArgs := []string{"lb", "lb", "create", "name=" + name, "description=" + description, "organization-id=" + orgID}
		for i, t := range tags {
			createArgs = append(createArgs, fmt.Sprintf("tags.%d=%s", i, t))
		}
		createArgs = append(createArgs, "zone="+zone)
		createRes, err := scwRunJSON(ctx, conn, createArgs...)
		if err != nil {
			return Result{}, err
		}
		if createRes.RC != 0 {
			return Fail("scaleway_lb: failed to create load-balancer " + name + ": " + scwErrMsg(createRes)), nil
		}
		var created map[string]any
		if derr := scwDecode(createRes.Stdout, &created); derr != nil {
			return Result{}, derr
		}
		return Changed("").WithExtra("lb", created), nil
	}

	id := scwLBStr(current, "id")
	changed := false
	if scwLBStr(current, "name") != name || scwLBStr(current, "description") != description {
		updRes, err := scwRunJSON(ctx, conn, "lb", "lb", "update", "lb-id="+id, "name="+name, "description="+description, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if updRes.RC != 0 {
			return Fail("scaleway_lb: failed to update load-balancer " + name + ": " + scwErrMsg(updRes)), nil
		}
		changed = true
		var updated map[string]any
		if derr := scwDecode(updRes.Stdout, &updated); derr == nil && updated != nil {
			current = updated
		}
	}
	res := Result{Changed: changed}
	return res.WithExtra("lb", current), nil
}

func scwLBStr(m map[string]any, key string) string {
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
