package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayPrivateNetwork implements Ansible's
// `scaleway_private_network` (community.general) module: creates/
// updates/deletes a Scaleway private network (VPC-equivalent), via
// `scw vpc private-network create/list/update/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI.
//
// Args: state (present|absent, default present); project (required);
// region (required, the SAME 13-choice zone-style enum
// scaleway_ip/scaleway_volume document, despite `scw vpc
// private-network`'s own genuinely-regional `region=` argument —
// verified fr-par/nl-ams/pl-waw, see scwVPCRegion below); name
// (optional); tags (list, default []).
//
// Deviation — name omitted: real get_private_network(api, name=None)
// would send name=None as a query filter to a real HTTP client, an
// edge case real scaleway_private_network's own source does not
// special-case either. Since this port cannot match "no name" against
// anything meaningful, an omitted/empty name always creates a new
// private network on state=present (matching scaleway_ip.go's own
// documented "no id means always create" stance) and is always
// Changed=false on state=absent (nothing nameable to find and delete).
//
// present: name given -> `scw vpc private-network list name=<name>
// region=<region-prefix> -o json`, confirmed exact match (scwFindByName).
// Not found -> `scw vpc private-network create name=<name>
// project-id=<project> tags.N=<tag> region=<region-prefix>`,
// Changed=true. Found -> compare current tags (set-equality, matching
// real present_strategy's own `set(wished["tags"]) == set(current["tags"])`)
// against wished; differ -> `scw vpc private-network update
// private-network-id=<id> name=<name> tags.N=<tag>
// region=<region-prefix>`, Changed=true.
//
// absent: name given and found -> `scw vpc private-network delete
// private-network-id=<id> region=<region-prefix>`, Changed=true;
// otherwise Changed=false.
//
// Extra["private_network"]: the created/current/updated object, decoded
// directly from `scw`'s own JSON output (see scaleway_common.go's own
// "Output shape" caveat).
func moduleScalewayPrivateNetwork(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_private_network"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	region, err := requireString(args, "region")
	if err != nil {
		return Result{}, err
	}
	regionPrefix, err := scwVPCRegion(region)
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_private_network: state must be present or absent, got %q", state)
	}
	name := argString(args, "name", "")
	tags := argStringList(args, "tags")

	var current map[string]any
	var found bool
	if name != "" {
		current, found, err = scwFindByName(ctx, conn, name, "vpc", "private-network", "list", "name="+name, "region="+regionPrefix)
		if err != nil {
			return Result{}, err
		}
	}

	if state == "absent" {
		if !found {
			return Ok(""), nil
		}
		id := scwPNStr(current, "id")
		delRes, err := scwRun(ctx, conn, "vpc", "private-network", "delete", "private-network-id="+id, "region="+regionPrefix)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_private_network: failed to delete private network " + name + ": " + scwErrMsg(delRes)), nil
		}
		return Changed(""), nil
	}

	if !found {
		createArgs := []string{"vpc", "private-network", "create", "name=" + name, "project-id=" + project}
		for i, t := range tags {
			createArgs = append(createArgs, fmt.Sprintf("tags.%d=%s", i, t))
		}
		createArgs = append(createArgs, "region="+regionPrefix)
		createRes, err := scwRunJSON(ctx, conn, createArgs...)
		if err != nil {
			return Result{}, err
		}
		if createRes.RC != 0 {
			return Fail("scaleway_private_network: failed to create private network: " + scwErrMsg(createRes)), nil
		}
		var created map[string]any
		if derr := scwDecode(createRes.Stdout, &created); derr != nil {
			return Result{}, derr
		}
		return Changed("").WithExtra("private_network", created), nil
	}

	if scwTagSetEqual(tags, scwPNStrList(current, "tags")) {
		res := Result{Changed: false}
		return res.WithExtra("private_network", current), nil
	}

	id := scwPNStr(current, "id")
	updArgs := []string{"vpc", "private-network", "update", "private-network-id=" + id, "name=" + name}
	for i, t := range tags {
		updArgs = append(updArgs, fmt.Sprintf("tags.%d=%s", i, t))
	}
	updArgs = append(updArgs, "region="+regionPrefix)
	updRes, err := scwRunJSON(ctx, conn, updArgs...)
	if err != nil {
		return Result{}, err
	}
	if updRes.RC != 0 {
		return Fail("scaleway_private_network: failed to update private network " + name + ": " + scwErrMsg(updRes)), nil
	}
	var updated map[string]any
	if derr := scwDecode(updRes.Stdout, &updated); derr != nil {
		return Result{}, derr
	}
	return Changed("").WithExtra("private_network", updated), nil
}

// scwVPCRegion resolves a legacy 13-choice region value down to the
// region-only prefix `scw vpc private-network`'s own genuinely-regional
// `region=` argument expects (e.g. "par1" -> "fr-par") — see
// moduleScalewayPrivateNetwork's own doc comment.
func scwVPCRegion(region string) (string, error) {
	zone, err := scwZone(region)
	if err != nil {
		return "", err
	}
	i := strings.LastIndex(zone, "-")
	if i < 0 {
		return zone, nil
	}
	return zone[:i], nil
}

func scwPNStr(m map[string]any, key string) string {
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

func scwPNStrList(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// scwTagSetEqual compares two tag lists as true SETS (matching real
// scaleway_private_network's own `set(a) == set(b)` comparison exactly
// — duplicate entries collapse, unlike a sorted-slice comparison).
func scwTagSetEqual(a, b []string) bool {
	sa := map[string]bool{}
	for _, t := range a {
		sa[t] = true
	}
	sb := map[string]bool{}
	for _, t := range b {
		sb[t] = true
	}
	if len(sa) != len(sb) {
		return false
	}
	for t := range sa {
		if !sb[t] {
			return false
		}
	}
	return true
}
