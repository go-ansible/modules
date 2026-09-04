package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayFunction implements Ansible's `scaleway_function`
// (community.general) module: creates, updates, or deletes a Scaleway
// Serverless Function within an existing namespace, via `scw function
// function create/list/update/delete/deploy` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_function's own direct REST API calls,
// and for the auth/region/wait/secret-variable deviations shared by
// every scaleway_* module in this batch.
//
// Args: namespace_id (required); name (required — see
// scaleway_container.go's own doc comment on why this port requires it
// even though real scaleway_function's own arg is not marked
// required); region (required, fr-par|nl-ams|pl-waw); runtime
// (required); handler; privacy (public|private, default "public");
// memory_limit, min_scale, max_scale (int, no default — see
// scaleway_container.go's own doc comment on this port's "only manage
// what was given" deviation, applied identically here); function_timeout
// (str, no default) — rendered as `scw`'s own `timeout=` argument (real
// scaleway_function's own arg is named function_timeout; the underlying
// API/CLI field is `timeout`, verified against scaleway_function.py's
// own payload_from_wished_fn); environment_variables/
// secret_environment_variables (dict, default {}); redeploy (bool,
// default false); state (present|absent, default present).
//
// present/absent/diff/redeploy semantics mirror scaleway_container.go's
// own module exactly (see that module's own doc comment) — real
// scaleway_function's own VERIFIABLE_MUTABLE_ATTRIBUTES is description/
// min_scale/max_scale/environment_variables/runtime/memory_limit/
// timeout/handler/privacy (verified against scaleway_function.py, not
// assumed from scaleway_container.py's own shape) — with no
// cpu_limit/protocol/port/max_concurrency at all (Scaleway Functions has
// no equivalent of those Container-only fields).
//
// Extra["function"] (state=present only) is `scw`'s own JSON object for
// the function.
func moduleScalewayFunction(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_function"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	namespaceID, err := requireString(args, "namespace_id")
	if err != nil {
		return Result{}, err
	}
	region, err := scwRegionArg(args, "scaleway_function")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_function: state must be one of present, absent, got %q", state)
	}

	current, exists, err := scwFindByName(ctx, conn, name,
		"function", "function", "list", "namespace-id="+namespaceID, "region="+region)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(""), nil
		}
		id, _ := current["id"].(string)
		res, err := scwRun(ctx, conn, "function", "function", "delete", "function-id="+id, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_function: failed to delete function " + name + ": " + scwErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	runtime, err := requireString(args, "runtime")
	if err != nil {
		return Result{}, err
	}
	handler := argString(args, "handler", "")
	description := argString(args, "description", "")
	privacy := argString(args, "privacy", "public")
	envVars := scwStringMap(args, "environment_variables")
	secretVars := scwStringMap(args, "secret_environment_variables")
	redeploy := argBool(args, "redeploy", false)

	optIntArgs := []struct{ arg, flag string }{
		{"memory_limit", "memory-limit"}, {"max_scale", "max-scale"}, {"min_scale", "min-scale"},
	}

	changed := false
	if !exists {
		argv := []string{"function", "function", "create", "name=" + name, "namespace-id=" + namespaceID,
			"region=" + region, "runtime=" + runtime, "description=" + description, "privacy=" + privacy}
		if handler != "" {
			argv = append(argv, "handler="+handler)
		}
		if v, ok := args["function_timeout"]; ok {
			argv = append(argv, "timeout="+argString(args, "function_timeout", fmt.Sprint(v)))
		}
		for _, o := range optIntArgs {
			if _, ok := args[o.arg]; ok {
				argv = append(argv, o.flag+"="+fmt.Sprint(argInt(args, o.arg, 0)))
			}
		}
		argv = append(argv, scwEnvTokens("environment-variables", envVars)...)
		argv = append(argv, scwSecretEnvTokens(secretVars)...)
		res, err := scwRunJSON(ctx, conn, argv...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_function: failed to create function " + name + ": " + scwErrMsg(res)), nil
		}
		if derr := scwDecode(res.Stdout, &current); derr != nil {
			return Result{}, derr
		}
		changed = true
	} else {
		diff := false
		curDesc, _ := current["description"].(string)
		curEnv := scwAnyStringMap(current["environment_variables"])
		curPrivacy, _ := current["privacy"].(string)
		curRuntime, _ := current["runtime"].(string)
		curHandler, _ := current["handler"].(string)
		if curDesc != description || !scwStringMapEqual(curEnv, envVars) || curPrivacy != privacy ||
			curRuntime != runtime || (handler != "" && curHandler != handler) {
			diff = true
		}
		if v, ok := args["function_timeout"]; ok {
			want := argString(args, "function_timeout", fmt.Sprint(v))
			if cur, _ := current["timeout"].(string); cur != want {
				diff = true
			}
		}
		for _, o := range optIntArgs {
			if _, ok := args[o.arg]; ok {
				want := argInt(args, o.arg, 0)
				if cur, ok := scwAnyInt(current[o.arg]); !ok || cur != want {
					diff = true
				}
			}
		}
		if diff {
			id, _ := current["id"].(string)
			argv := []string{"function", "function", "update", "function-id=" + id, "region=" + region,
				"runtime=" + runtime, "description=" + description, "privacy=" + privacy}
			if handler != "" {
				argv = append(argv, "handler="+handler)
			}
			if v, ok := args["function_timeout"]; ok {
				argv = append(argv, "timeout="+argString(args, "function_timeout", fmt.Sprint(v)))
			}
			for _, o := range optIntArgs {
				if _, ok := args[o.arg]; ok {
					argv = append(argv, o.flag+"="+fmt.Sprint(argInt(args, o.arg, 0)))
				}
			}
			argv = append(argv, scwEnvTokens("environment-variables", envVars)...)
			argv = append(argv, scwSecretEnvTokens(secretVars)...)
			res, err := scwRunJSON(ctx, conn, argv...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("scaleway_function: failed to update function " + name + ": " + scwErrMsg(res)), nil
			}
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
			changed = true

			if redeploy {
				id, _ := current["id"].(string)
				dres, err := scwRunJSON(ctx, conn, "function", "function", "deploy", "function-id="+id, "region="+region)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("scaleway_function: failed to redeploy function " + name + ": " + scwErrMsg(dres)), nil
				}
				if derr := scwDecode(dres.Stdout, &current); derr != nil {
					return Result{}, derr
				}
			}
		}
	}

	r := Result{Changed: changed}
	return r.WithExtra("function", current), nil
}
