package modules

import (
	"context"
	"encoding/json"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePipxInfo implements Ansible's `pipx_info` module: a read-only
// counterpart to pipx.go, reusing its pipxListApplications/
// pipxCheckVersion helpers (see pipx.go's own doc comment for the
// `pipx list --include-injected --json` shape and version-gate both
// modules share). Like package_facts.go/pip_package_info.go, this
// module never writes anything and always reports Changed=false.
//
// Args: name (string, optional) — filter to one application; executable
// (string, default "pipx" — see pipx.go's own doc comment for why this
// port does not replicate real pipx_info's `python -m pipx` default);
// global (bool, default false) — `--global`; include_deps (bool,
// default false); include_injected (bool, default false); include_raw
// (bool, default false) — also returns the raw parsed JSON document in
// Extra["raw_output"].
//
// Extra["application"] is a list of maps (name/version/pinned, plus
// dependencies/injected when requested), matching real pipx_info's own
// RETURN shape; Extra["cmd"] echoes the argv this port used to list
// applications.
func modulePipxInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	exe := argString(args, "executable", "pipx")
	if exe == "" {
		exe = "pipx"
	}
	global := argBool(args, "global", false)
	if err := pipxCheckVersion(ctx, conn, exe, global); err != nil {
		return Result{}, err
	}

	includeDeps := argBool(args, "include_deps", false)
	includeInjected := argBool(args, "include_injected", false)
	includeRaw := argBool(args, "include_raw", false)
	name := argString(args, "name", "")

	out, err := pipxListJSON(ctx, conn, exe, global)
	if err != nil {
		return Result{}, err
	}
	apps, err := pipxParseList(out, includeInjected, includeDeps)
	if err != nil {
		return Result{}, err
	}

	var application []any
	for appName, app := range apps {
		if name != "" && appName != name {
			continue
		}
		entry := map[string]any{"name": appName, "version": app.Version}
		if app.Pinned != nil {
			entry["pinned"] = *app.Pinned
		}
		if includeInjected {
			injected := map[string]any{}
			for k, v := range app.Injected {
				injected[k] = v
			}
			entry["injected"] = injected
		}
		if includeDeps {
			deps := make([]any, len(app.Dependencies))
			for i, d := range app.Dependencies {
				deps[i] = d
			}
			entry["dependencies"] = deps
		}
		application = append(application, entry)
	}
	if application == nil {
		application = []any{}
	}

	cmdList := []any{exe}
	if global {
		cmdList = append(cmdList, "--global")
	}
	cmdList = append(cmdList, "list", "--include-injected", "--json")

	res := Ok("").WithExtra("application", application).WithExtra("cmd", cmdList)
	if includeRaw {
		var raw any
		if err := json.Unmarshal([]byte(out), &raw); err == nil {
			res = res.WithExtra("raw_output", raw)
		}
	}
	return res, nil
}
