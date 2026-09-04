package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayContainerNamespace implements Ansible's
// `scaleway_container_namespace` (community.general) module: creates,
// updates, or deletes a Scaleway Serverless Containers namespace, via
// `scw container namespace create/list/update/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_container_namespace's own direct
// REST API calls, and for the auth/region/wait/secret-variable
// deviations shared by every scaleway_* module in this batch.
//
// Args: name (required); project_id (required); region (required,
// fr-par|nl-ams|pl-waw); description (default ""); environment_variables
// (dict, default {}); secret_environment_variables (dict, default {});
// state (present|absent, default present); api_token/api_url/profile/
// api_timeout/validate_certs/query_parameters/wait/wait_timeout/
// wait_sleep_time accepted, no effect (see scaleway_common.go).
//
// present: looks the namespace up by exact name within project_id
// (`scw container namespace list project-id=... region=...`, matching
// real present_strategy's own fetch_all_resources+name-lookup); creates
// it if missing. Whether freshly created or pre-existing, this port
// diffs the CURRENT namespace's own description/environment_variables
// against the requested values (secret_environment_variables is always
// sent but never compared — see scaleway_common.go's own doc comment on
// why, matching real MUTABLE_ATTRIBUTES's own SecretVariables.decode
// substitution exactly) and issues `scw container namespace update`
// only for an actual difference.
//
// absent: looks the namespace up the same way; `scw container namespace
// delete namespace-id=... region=...` if found (Changed=true), else
// Changed=false — matching real absent_strategy's own no-op-if-missing
// handling.
//
// Extra["container_namespace"] (state=present only) is `scw`'s own JSON
// object for the namespace — see scaleway_container_namespace.go's own
// module doc comment sibling files carry for the equivalent field-shape
// deviation from real module_utils' raw REST response.
func moduleScalewayContainerNamespace(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_container_namespace"); !ok {
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
	region, err := scwRegionArg(args, "scaleway_container_namespace")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_container_namespace: state must be one of present, absent, got %q", state)
	}
	description := argString(args, "description", "")
	envVars := scwStringMap(args, "environment_variables")
	secretVars := scwStringMap(args, "secret_environment_variables")

	current, exists, err := scwFindByName(ctx, conn, name,
		"container", "namespace", "list", "project-id="+projectID, "region="+region)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(""), nil
		}
		id, _ := current["id"].(string)
		res, err := scwRun(ctx, conn, "container", "namespace", "delete", "namespace-id="+id, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_container_namespace: failed to delete namespace " + name + ": " + scwErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	changed := false
	if !exists {
		argv := []string{"container", "namespace", "create", "name=" + name, "project-id=" + projectID,
			"region=" + region, "description=" + description}
		argv = append(argv, scwEnvTokens("environment-variables", envVars)...)
		argv = append(argv, scwSecretEnvTokens(secretVars)...)
		res, err := scwRunJSON(ctx, conn, argv...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_container_namespace: failed to create namespace " + name + ": " + scwErrMsg(res)), nil
		}
		if derr := scwDecode(res.Stdout, &current); derr != nil {
			return Result{}, derr
		}
		changed = true
	} else {
		curDesc, _ := current["description"].(string)
		curEnv := scwAnyStringMap(current["environment_variables"])
		if curDesc != description || !scwStringMapEqual(curEnv, envVars) {
			id, _ := current["id"].(string)
			argv := []string{"container", "namespace", "update", "namespace-id=" + id, "region=" + region,
				"description=" + description}
			argv = append(argv, scwEnvTokens("environment-variables", envVars)...)
			argv = append(argv, scwSecretEnvTokens(secretVars)...)
			res, err := scwRunJSON(ctx, conn, argv...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("scaleway_container_namespace: failed to update namespace " + name + ": " + scwErrMsg(res)), nil
			}
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
			changed = true
		}
	}

	r := Result{Changed: changed}
	return r.WithExtra("container_namespace", current), nil
}
