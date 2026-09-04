package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayContainer implements Ansible's `scaleway_container`
// (community.general) module: creates, updates, or deletes a Scaleway
// Serverless Container within an existing namespace, via `scw container
// container create/list/update/delete/deploy` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_container's own direct REST API
// calls, and for the auth/region/wait/secret-variable deviations
// shared by every scaleway_* module in this batch.
//
// Args: namespace_id (required); name (required — this port's own
// find-by-name lookup needs it, though real scaleway_container's own
// arg is not marked required); region (required, fr-par|nl-ams|pl-waw);
// registry_image (required); description (default ""); privacy
// (public|private, default "public"); protocol (http1|h2c, default
// "http1"); port, cpu_limit, memory_limit, max_concurrency, max_scale,
// min_scale (int, no default — see below); container_timeout (str, no
// default) — rendered as `scw`'s own `timeout=` argument (real
// scaleway_container's own arg is named container_timeout; the
// underlying API/CLI field is `timeout` — verified against
// scaleway_container.py's own payload_from_wished_cn, not guessed);
// environment_variables/secret_environment_variables (dict, default
// {}); redeploy (bool, default false); state (present|absent, default
// present).
//
// Deviation — optional scalar fields (port/cpu_limit/memory_limit/
// max_concurrency/max_scale/min_scale/container_timeout): real
// scaleway_container always sends every one of these in its own create/
// update payload (None where unset, letting the API apply its own
// server-side default). This port instead only ever places a `scw`
// arg=value token for one of these when the Ansible argument was
// actually GIVEN — matching every other module in this package's own
// "omitted means don't touch" convention (see ipa_common.go's own doc
// comment on ipaScalarDiff) — since this port has no live namespace to
// verify each field's own server-side default against. A caller relying
// on real scaleway_container's own None-means-"use the API default"
// behavior for one of these should pass it explicitly instead.
//
// present: looks the container up by exact name within namespace_id
// (`scw container container list namespace-id=... region=...`);
// creates it if missing (redeploy is never sent at create — matching
// real payload_from_wished_cn's own `del payload_cn["redeploy"]` for a
// fresh create, verified against scaleway_container.py). Whether
// freshly created or pre-existing, this port diffs the CURRENT
// container's own description/min_scale/max_scale/environment_variables/
// cpu_limit/memory_limit/timeout/privacy/registry_image/
// max_concurrency/protocol/port against the requested (given) values —
// real MUTABLE_ATTRIBUTES exactly, minus secret_environment_variables
// (see scaleway_common.go's own doc comment on why that one is never
// diffed) — and issues `scw container container update` only for an
// actual difference. If anything was updated AND redeploy=true, this
// port then runs `scw container container deploy` — matching real
// scaleway_container's own documented "Redeploy the container if
// update is required" contract.
//
// absent: looks the container up the same way; `scw container
// container delete container-id=... region=...` if found
// (Changed=true), else Changed=false.
//
// Extra["container"] (state=present only) is `scw`'s own JSON object
// for the container.
func moduleScalewayContainer(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_container"); !ok {
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
	region, err := scwRegionArg(args, "scaleway_container")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_container: state must be one of present, absent, got %q", state)
	}

	current, exists, err := scwFindByName(ctx, conn, name,
		"container", "container", "list", "namespace-id="+namespaceID, "region="+region)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(""), nil
		}
		id, _ := current["id"].(string)
		res, err := scwRun(ctx, conn, "container", "container", "delete", "container-id="+id, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_container: failed to delete container " + name + ": " + scwErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	registryImage, err := requireString(args, "registry_image")
	if err != nil {
		return Result{}, err
	}
	description := argString(args, "description", "")
	privacy := argString(args, "privacy", "public")
	protocol := argString(args, "protocol", "http1")
	envVars := scwStringMap(args, "environment_variables")
	secretVars := scwStringMap(args, "secret_environment_variables")
	redeploy := argBool(args, "redeploy", false)

	// optIntArgs/optStrArgs: ansible-arg-name -> scw-flag-name, only
	// emitted (and only diffed) when the caller actually gave a value —
	// see this module's own doc comment.
	optIntArgs := []struct{ arg, flag string }{
		{"port", "port"}, {"cpu_limit", "cpu-limit"}, {"memory_limit", "memory-limit"},
		{"max_concurrency", "max-concurrency"}, {"max_scale", "max-scale"}, {"min_scale", "min-scale"},
	}

	changed := false
	if !exists {
		argv := []string{"container", "container", "create", "name=" + name, "namespace-id=" + namespaceID,
			"region=" + region, "registry-image=" + registryImage, "description=" + description,
			"privacy=" + privacy, "protocol=" + protocol}
		if v, ok := args["container_timeout"]; ok {
			argv = append(argv, "timeout="+argString(args, "container_timeout", fmt.Sprint(v)))
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
			return Fail("scaleway_container: failed to create container " + name + ": " + scwErrMsg(res)), nil
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
		curProtocol, _ := current["protocol"].(string)
		curRegistryImage, _ := current["registry_image"].(string)
		if curDesc != description || !scwStringMapEqual(curEnv, envVars) || curPrivacy != privacy ||
			curProtocol != protocol || curRegistryImage != registryImage {
			diff = true
		}
		if v, ok := args["container_timeout"]; ok {
			want := argString(args, "container_timeout", fmt.Sprint(v))
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
			argv := []string{"container", "container", "update", "container-id=" + id, "region=" + region,
				"registry-image=" + registryImage, "description=" + description, "privacy=" + privacy,
				"protocol=" + protocol}
			if v, ok := args["container_timeout"]; ok {
				argv = append(argv, "timeout="+argString(args, "container_timeout", fmt.Sprint(v)))
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
				return Fail("scaleway_container: failed to update container " + name + ": " + scwErrMsg(res)), nil
			}
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
			changed = true

			if redeploy {
				id, _ := current["id"].(string)
				dres, err := scwRunJSON(ctx, conn, "container", "container", "deploy", "container-id="+id, "region="+region)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("scaleway_container: failed to redeploy container " + name + ": " + scwErrMsg(dres)), nil
				}
				if derr := scwDecode(dres.Stdout, &current); derr != nil {
					return Result{}, derr
				}
			}
		}
	}

	r := Result{Changed: changed}
	return r.WithExtra("container", current), nil
}
