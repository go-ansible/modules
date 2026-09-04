package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayContainerRegistry implements Ansible's
// `scaleway_container_registry` (community.general) module: creates,
// updates, or deletes a Scaleway Container Registry namespace, via `scw
// registry namespace create/list/update/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_container_registry's own direct REST
// API calls, and for the auth/region/wait deviations shared by every
// scaleway_* module in this batch.
//
// Args: name (required); project_id (required); region (required,
// fr-par|nl-ams|pl-waw); description (default ""); privacy_policy
// (public|private, default "private") — rendered as `scw`'s own
// `is-public=true|false` argument (real MUTABLE_ATTRIBUTES's own
// `is_public: wished_cr["privacy_policy"] == "public"` derivation,
// verified against the real module's own source, not guessed); state
// (present|absent, default present); api_token/api_url/profile/
// api_timeout/validate_certs/query_parameters/wait/wait_timeout/
// wait_sleep_time accepted, no effect (see scaleway_common.go).
//
// present: looks the registry namespace up by exact name within
// project_id; creates it if missing. Whether freshly created or
// pre-existing, this port diffs the CURRENT namespace's own
// description/is_public against the requested values (real
// MUTABLE_ATTRIBUTES exactly: description, is_public — verified against
// scaleway_container_registry.py) and issues `scw registry namespace
// update` only for an actual difference.
//
// absent: looks the namespace up the same way; `scw registry namespace
// delete namespace-id=... region=...` if found (Changed=true), else
// Changed=false.
//
// Extra["container_registry"] (state=present only) is `scw`'s own JSON
// object for the registry namespace.
func moduleScalewayContainerRegistry(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_container_registry"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	projectID, err := requireString(args, "project_id")
	if err != nil {
		return Result{}, err
	}
	region, err := scwRegionArg(args, "scaleway_container_registry")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_container_registry: state must be one of present, absent, got %q", state)
	}
	description := argString(args, "description", "")
	privacyPolicy := argString(args, "privacy_policy", "private")
	if privacyPolicy != "public" && privacyPolicy != "private" {
		return Result{}, errArg("scaleway_container_registry: privacy_policy must be one of public, private, got %q", privacyPolicy)
	}
	isPublic := privacyPolicy == "public"

	current, exists, err := scwFindByName(ctx, conn, name,
		"registry", "namespace", "list", "project-id="+projectID, "region="+region)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(""), nil
		}
		id, _ := current["id"].(string)
		res, err := scwRun(ctx, conn, "registry", "namespace", "delete", "namespace-id="+id, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_container_registry: failed to delete registry " + name + ": " + scwErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	changed := false
	if !exists {
		res, err := scwRunJSON(ctx, conn, "registry", "namespace", "create", "name="+name,
			"project-id="+projectID, "region="+region, "description="+description,
			"is-public="+strconv.FormatBool(isPublic))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_container_registry: failed to create registry " + name + ": " + scwErrMsg(res)), nil
		}
		if derr := scwDecode(res.Stdout, &current); derr != nil {
			return Result{}, derr
		}
		changed = true
	} else {
		curDesc, _ := current["description"].(string)
		curPublic, _ := current["is_public"].(bool)
		if curDesc != description || curPublic != isPublic {
			id, _ := current["id"].(string)
			res, err := scwRunJSON(ctx, conn, "registry", "namespace", "update", "namespace-id="+id,
				"region="+region, "description="+description, "is-public="+strconv.FormatBool(isPublic))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("scaleway_container_registry: failed to update registry " + name + ": " + scwErrMsg(res)), nil
			}
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
			changed = true
		}
	}

	r := Result{Changed: changed}
	return r.WithExtra("container_registry", current), nil
}
