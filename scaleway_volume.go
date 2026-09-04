package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayVolume implements Ansible's `scaleway_volume`
// (community.general) module: creates/deletes a Scaleway block-storage
// volume, via `scw instance volume create/list/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI, and for the region->zone mapping (scwZone) used below.
//
// Args: state (present|absent, default present); region (required);
// name (required); project; organization (mutually exclusive, but
// UNLIKE scaleway_ip/scaleway_security_group, NOT required_one_of —
// real scaleway_volume's own argument_spec has no required_one_of at
// all, verified directly against its source; both may be omitted,
// in which case the effective project used for matching/creation is
// "", matching real core()'s own `if project is None: project =
// organization` producing None either way).
//
// Fidelity note — no update path: real code only creates or no-ops on
// an exact (project, name) match; it never diffs/patches size/
// volume_type on an existing volume — verified directly against
// scaleway_volume.py's own source (no PATCH call exists at all).
//
// Fidelity note — last-match-wins: real code's own lookup loop
// (`for volume in volumes_json["volumes"]: if volume["project"] ==
// project and volume["name"] == name: volumeByName = volume`) keeps
// overwriting volumeByName on every match, so if more than one volume
// somehow shares both (project, name), the LAST one in API list order
// wins, not the first — this port replicates that exact tie-break.
//
// present: `scw instance volume list zone=<zone> -o json` (unfiltered
// — matching real code's own unfiltered GET), filtered client-side by
// (project, name) with last-match-wins. Found -> Changed=false. Not
// found -> `scw instance volume create name=<name> project-id=<project>
// volume-type=<volume_type> size=<size> zone=<zone>`, Changed=true.
//
// absent: not found -> Changed=false; found -> `scw instance volume
// delete volume-id=<id> zone=<zone>`, Changed=true.
//
// Extra["volume"]: the current/created object, decoded directly from
// `scw`'s own JSON output (see scaleway_common.go's own "Output shape"
// caveat).
func moduleScalewayVolume(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_volume"); !ok {
		return res, nil
	}
	region, err := requireString(args, "region")
	if err != nil {
		return Result{}, err
	}
	zone, err := scwZone(region)
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	project := argString(args, "project", "")
	organization := argString(args, "organization", "")
	if project != "" && organization != "" {
		return Result{}, errArg("scaleway_volume: organization and project are mutually exclusive")
	}
	if project == "" {
		project = organization
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_volume: state must be present or absent, got %q", state)
	}
	size := argInt(args, "size", 0)
	volumeType := argString(args, "volume_type", "")

	listRes, err := scwRunJSON(ctx, conn, "instance", "volume", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if listRes.RC != 0 {
		return Fail("scaleway_volume: failed to list volumes: " + scwErrMsg(listRes)), nil
	}
	var volumes []map[string]any
	if derr := scwDecode(listRes.Stdout, &volumes); derr != nil {
		return Result{}, derr
	}
	var current map[string]any
	for _, v := range volumes {
		if scwVolumeStr(v, "project") == project && scwVolumeStr(v, "name") == name {
			current = v
		}
	}

	if state == "absent" {
		if current == nil {
			return Ok(""), nil
		}
		id := scwVolumeStr(current, "id")
		delRes, err := scwRun(ctx, conn, "instance", "volume", "delete", "volume-id="+id, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_volume: failed to delete volume " + name + ": " + scwErrMsg(delRes)), nil
		}
		return Changed(""), nil
	}

	if current != nil {
		res := Result{Changed: false}
		return res.WithExtra("volume", current), nil
	}

	createArgs := []string{"instance", "volume", "create", "name=" + name}
	if project != "" {
		createArgs = append(createArgs, "project-id="+project)
	}
	if volumeType != "" {
		createArgs = append(createArgs, "volume-type="+volumeType)
	}
	if size > 0 {
		createArgs = append(createArgs, "size="+strconv.Itoa(size))
	}
	createArgs = append(createArgs, "zone="+zone)
	createRes, err := scwRunJSON(ctx, conn, createArgs...)
	if err != nil {
		return Result{}, err
	}
	if createRes.RC != 0 {
		return Fail("scaleway_volume: failed to create volume " + name + ": " + scwErrMsg(createRes)), nil
	}
	var created map[string]any
	if derr := scwDecode(createRes.Stdout, &created); derr != nil {
		return Result{}, derr
	}
	return Changed("").WithExtra("volume", created), nil
}

func scwVolumeStr(m map[string]any, key string) string {
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
