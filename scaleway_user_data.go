package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayUserData implements Ansible's `scaleway_user_data`
// (community.general) module: manages an instance's cloud-init/
// user-data key/value payload, via `scw instance user-data set/get/
// list/delete` — see scaleway_common.go's own doc comment for why this
// port substitutes the `scw` CLI, and for the region->zone mapping
// (scwZone) used below.
//
// Args: region (required); server_id (required); user_data (a dict of
// string key -> string value).
//
// Fidelity note — user_data is effectively required in practice: real
// scaleway_user_data's own argument_spec does not mark `user_data` as
// required (default null), but its own core() unconditionally does
// `for key in present_user_data: if key not in user_data` — a plain
// dict-membership test against user_data — which raises a Python
// TypeError if user_data is None and there is ANY existing user-data
// key at all, and unconditionally does `user_data.items()` right after
// (an AttributeError on None even with nothing existing) — verified
// directly against scaleway_user_data.py's own source, which has no
// `if user_data is None` guard anywhere. This port makes that
// practical requirement explicit: user_data missing/null is an argErr,
// not a silent no-op.
//
// Reconciliation: real core() first DELETES every existing key not
// present in the wished user_data, then SETS every wished key whose
// value differs from (or is absent from) the existing set — this port
// replicates that exact two-pass order. `scw instance user-data list
// server-id=<server_id> zone=<zone> -o json` supplies the existing key
// NAMES; `scw instance user-data get server-id=<server_id> key=<key>
// zone=<zone>` (run WITHOUT -o json — user-data content is an arbitrary
// text/plain blob, not a structured resource, so this port reads its
// raw stdout directly rather than assuming a JSON envelope, unlike
// every other read in this batch) supplies each existing key's current
// value for the diff.
//
// Deviation — `scw instance user-data list`'s own exact JSON shape for
// a plain string-array resource (as opposed to a typed API resource)
// was not independently verified against a live `scw` binary in this
// sandbox (none is installed here); this port assumes — consistent with
// scaleway_common.go's own general "Output shape" stance for `-o
// json` — a bare JSON array of key-name strings.
//
// present: any delete or set issued -> Changed=true; user_data already
// matches present_user_data exactly (real core()'s own `if
// present_user_data == user_data` short-circuit) -> Changed=false. This
// module has no `state` argument (real scaleway_user_data has none
// either — an empty user_data dict removes every key, matching its own
// documented "user_data: {}" semantics).
//
// Extra["user_data"]: the wished user_data map on a changed/no-op run.
func moduleScalewayUserData(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_user_data"); !ok {
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
	serverID, err := requireString(args, "server_id")
	if err != nil {
		return Result{}, err
	}
	if v, ok := args["user_data"]; !ok || v == nil {
		return Result{}, errArg("scaleway_user_data: user_data is required (pass {} to clear all keys) — " +
			"real scaleway_user_data's own source has no None-safe path for an omitted user_data, see this " +
			"module's own doc comment")
	}
	userData := scwStringMap(args, "user_data")
	if userData == nil {
		userData = map[string]string{}
	}

	listRes, err := scwRunJSON(ctx, conn, "instance", "user-data", "list", "server-id="+serverID, "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if listRes.RC != 0 {
		return Fail("scaleway_user_data: failed to list user-data keys for " + serverID + ": " + scwErrMsg(listRes)), nil
	}
	var existingKeys []string
	if derr := scwDecode(listRes.Stdout, &existingKeys); derr != nil {
		return Result{}, derr
	}

	present := map[string]string{}
	for _, key := range existingKeys {
		getRes, err := scwRun(ctx, conn, "instance", "user-data", "get", "server-id="+serverID, "key="+key, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if getRes.RC != 0 {
			return Fail("scaleway_user_data: failed to read user-data key " + key + ": " + scwErrMsg(getRes)), nil
		}
		present[key] = getRes.Stdout
	}

	if scwStringMapEqual(present, userData) {
		return Ok("").WithExtra("user_data", userData), nil
	}

	changed := false
	for _, key := range existingKeys {
		if _, ok := userData[key]; !ok {
			delRes, err := scwRun(ctx, conn, "instance", "user-data", "delete", "server-id="+serverID, "key="+key, "zone="+zone)
			if err != nil {
				return Result{}, err
			}
			if delRes.RC != 0 {
				return Fail("scaleway_user_data: failed to delete user-data key " + key + ": " + scwErrMsg(delRes)), nil
			}
			changed = true
		}
	}
	for key, value := range userData {
		if cur, ok := present[key]; !ok || cur != value {
			setRes, err := scwRun(ctx, conn, "instance", "user-data", "set", "server-id="+serverID, "key="+key, "content="+value, "zone="+zone)
			if err != nil {
				return Result{}, err
			}
			if setRes.RC != 0 {
				return Fail("scaleway_user_data: failed to set user-data key " + key + ": " + scwErrMsg(setRes)), nil
			}
			changed = true
		}
	}

	res := Result{Changed: changed}
	return res.WithExtra("user_data", userData), nil
}
