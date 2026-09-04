package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayFunctionNamespace implements Ansible's
// `scaleway_function_namespace` (community.general) module: creates,
// updates, or deletes a Scaleway Serverless Functions namespace, via
// `scw function namespace create/list/update/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_function_namespace's own direct REST
// API calls, and for the auth/region/wait/secret-variable deviations
// shared by every scaleway_* module in this batch.
//
// Args, present/absent semantics, diffing, and Extra shape are all
// identical to scaleway_container_namespace.go's own module (real
// scaleway_function_namespace and scaleway_container_namespace share
// the exact same argument_spec and MUTABLE_ATTRIBUTES —
// description/environment_variables, with
// secret_environment_variables always sent but never diffed — down to
// the return-value key naming convention: function_namespace here vs.
// container_namespace there); see that module's own doc comment for
// the full explanation this one does not repeat. The only functional
// difference is the `scw` resource family this port shells out to:
// `function namespace` (Scaleway's Functions product) instead of
// `container namespace` (Scaleway's Containers product) — two
// separate Scaleway APIs with parallel namespace shapes, verified
// against scaleway-cli's own docs/commands/function.md, not assumed
// from scaleway_container_namespace.go's own shape alone.
//
// Extra["function_namespace"] (state=present only) is `scw`'s own JSON
// object for the namespace.
func moduleScalewayFunctionNamespace(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_function_namespace"); !ok {
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
	region, err := scwRegionArg(args, "scaleway_function_namespace")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_function_namespace: state must be one of present, absent, got %q", state)
	}
	description := argString(args, "description", "")
	envVars := scwStringMap(args, "environment_variables")
	secretVars := scwStringMap(args, "secret_environment_variables")

	current, exists, err := scwFindByName(ctx, conn, name,
		"function", "namespace", "list", "project-id="+projectID, "region="+region)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(""), nil
		}
		id, _ := current["id"].(string)
		res, err := scwRun(ctx, conn, "function", "namespace", "delete", "namespace-id="+id, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_function_namespace: failed to delete namespace " + name + ": " + scwErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	changed := false
	if !exists {
		argv := []string{"function", "namespace", "create", "name=" + name, "project-id=" + projectID,
			"region=" + region, "description=" + description}
		argv = append(argv, scwEnvTokens("environment-variables", envVars)...)
		argv = append(argv, scwSecretEnvTokens(secretVars)...)
		res, err := scwRunJSON(ctx, conn, argv...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_function_namespace: failed to create namespace " + name + ": " + scwErrMsg(res)), nil
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
			argv := []string{"function", "namespace", "update", "namespace-id=" + id, "region=" + region,
				"description=" + description}
			argv = append(argv, scwEnvTokens("environment-variables", envVars)...)
			argv = append(argv, scwSecretEnvTokens(secretVars)...)
			res, err := scwRunJSON(ctx, conn, argv...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("scaleway_function_namespace: failed to update namespace " + name + ": " + scwErrMsg(res)), nil
			}
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
			changed = true
		}
	}

	r := Result{Changed: changed}
	return r.WithExtra("function_namespace", current), nil
}
