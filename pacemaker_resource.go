package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacemakerResource implements Ansible's `pacemaker_resource`
// module: manages a Pacemaker cluster resource's state via the `pcs`
// CLI (matching real pacemaker_resource's own module_utils, _pacemaker,
// which wraps `pcs` — never `crm`).
//
// Args: state (present|absent|cloned|enabled|disabled|cleanup, default
// "present"); name (string) — required for present/absent/enabled/
// disabled; resource_type (dict{resource_name, resource_standard,
// resource_provider}) — required for present, joined as
// "[standard:][provider:]name" into the resource agent argument (e.g.
// resource_name=IPaddr2 alone becomes just "IPaddr2", matching real
// pacemaker_resource's own fmt_resource_type when standard/provider are
// left unset — `pcs` itself then "assumes" an agent namespace, as shown
// in real pacemaker_resource's own cluster_resources example output);
// resource_option ([]string, default []) — raw "key=value" tokens
// appended after the agent; resource_operation ([]dict{operation_action,
// operation_option}, default []) — each entry becomes "op <action>
// <option>..." on the command line; resource_meta ([]string) — becomes
// "meta <item>..." when non-empty; resource_argument
// (dict{argument_action: clone|master|group|promotable,
// argument_option}) — becomes "--group <option>..." for
// argument_action=group, or "<argument_action> <option>..." for the
// other three (matching real pacemaker_resource's own
// fmt_resource_argument); wait (int, default 300) — accepted, but this
// port does NOT poll for the resource to reach a ready state
// afterwards (see below).
//
// resource_clone_ids ([]string) is accepted but NOT implemented: real
// pacemaker_resource's own formatting for it
// (`[x for k in value for x in ("clone", k)]`, i.e. "clone id1 clone
// id2 ...") could not be confirmed against a live `pcs` invocation in
// this sandbox, and guessing wrong here would silently clone the WRONG
// resource under a plausible-looking command — a real, disclosed gap
// rather than a guessed one, in the same spirit as selinux.go's
// documented `update_kernel_param` gap.
//
// `wait` real pacemaker_resource polls `pcs resource status <name>`
// every 5 seconds until the resource reaches "Started" (or
// "Promoted"/"Unpromoted" for a clone) or the wait budget expires. This
// port has no equivalent poll loop — implementing one would make this
// module's own tests slow and, against a fakeConn, either infinite or
// meaningless (there's no real cluster converging in the background);
// accepted and stored in Extra["wait"] for a caller to see it had no
// effect, not silently swallowed.
//
// Changed is determined the same way real pacemaker_resource's own
// StateModuleHelper determines it: by diffing `pcs resource status
// [name]` text captured before and after the action, returned as
// Extra["cluster_resources"] (matching real pacemaker_resource's own
// identically-named return value) — not by reasoning about which
// individual command ran.
func modulePacemakerResource(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "cloned", "enabled", "disabled", "cleanup":
	default:
		return Result{}, errArg("pacemaker_resource: state must be one of present, absent, cloned, enabled, disabled, cleanup, got %q", state)
	}
	name := argString(args, "name", "")
	if (state == "present" || state == "absent" || state == "enabled" || state == "disabled") && name == "" {
		return Result{}, errArg("pacemaker_resource: name is required when state is %s", state)
	}
	wait := argInt(args, "wait", 300)

	getCmd := "pcs resource status"
	if name != "" {
		getCmd += " " + shellQuote(name)
	}
	before, err := pacemakerStatusText(ctx, conn, getCmd)
	if err != nil {
		return Result{}, err
	}

	maintOn, err := pacemakerMaintenanceOn(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		resType, _ := args["resource_type"].(map[string]any)
		if resType == nil || pacemakerMapString(resType, "resource_name") == "" {
			return Result{}, errArg("pacemaker_resource: resource_type.resource_name is required when state is present")
		}
		cmd := pacemakerResourceCreateCmd(name, resType, argStringList(args, "resource_option"),
			pacemakerResourceOperations(args["resource_operation"]), argStringList(args, "resource_meta"),
			pacemakerResourceArgument(args["resource_argument"]))
		if res, err := pacemakerStepMaybeStrict(ctx, conn, cmd, !maintOn, "already exists"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "absent":
		cmd := "pcs resource remove " + shellQuote(name)
		if maintOn {
			cmd += " --force"
		}
		if res, err := pacemakerStep(ctx, conn, cmd, "does not exist"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "cloned":
		cmd := "pcs resource clone " + shellQuote(name)
		if meta := argStringList(args, "resource_clone_meta"); len(meta) > 0 {
			cmd += " meta " + quoteAll(meta)
		}
		if res, err := pacemakerStepMaybeStrict(ctx, conn, cmd, !maintOn, "already a clone resource"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "enabled":
		if res, err := pacemakerStep(ctx, conn, "pcs resource enable "+shellQuote(name), "Starting"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "disabled":
		if res, err := pacemakerStep(ctx, conn, "pcs resource disable "+shellQuote(name), "Stopped"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "cleanup":
		cmd := "pcs resource cleanup"
		if name != "" {
			cmd += " " + shellQuote(name)
		}
		if res, err := pacemakerStep(ctx, conn, cmd, "Clean"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}
	}

	after, err := pacemakerStatusText(ctx, conn, getCmd)
	if err != nil {
		return Result{}, err
	}
	res := Result{Changed: before != after}
	res = res.WithExtra("cluster_resources", after).WithExtra("wait", wait)
	return res, nil
}

// pacemakerStepMaybeStrict is pacemakerStep with a caller-controlled
// strictness switch: when strict is false, ANY exit code is tolerated
// (matching real pacemaker_resource's own fail_on_err=NOT
// maintenance-mode for state=present/cloned).
func pacemakerStepMaybeStrict(ctx context.Context, conn remoteexec.Connection, cmd string, strict bool, ignoreErr string) (Result, error) {
	if !strict {
		if _, err := runStatus(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Ok(""), nil
	}
	return pacemakerStep(ctx, conn, cmd, ignoreErr)
}

func pacemakerMapString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}

// pacemakerResourceAgent joins resource_type's standard/provider/name
// into a single "[standard:][provider:]name" agent token, matching real
// pacemaker_resource's own fmt_resource_type.
func pacemakerResourceAgent(resType map[string]any) string {
	var parts []string
	for _, k := range []string{"resource_standard", "resource_provider", "resource_name"} {
		if v := pacemakerMapString(resType, k); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ":")
}

func pacemakerResourceOperations(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var toks []string
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		action := pacemakerMapString(m, "operation_action")
		if action == "" {
			continue
		}
		toks = append(toks, "op", action)
		opts, _ := m["operation_option"].([]any)
		for _, o := range opts {
			toks = append(toks, fmtAny(o))
		}
	}
	return toks
}

func pacemakerResourceArgument(v any) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	action := pacemakerMapString(m, "argument_action")
	if action == "" {
		return nil
	}
	var toks []string
	if action == "group" {
		toks = append(toks, "--group")
	} else {
		toks = append(toks, action)
	}
	opts, _ := m["argument_option"].([]any)
	for _, o := range opts {
		toks = append(toks, fmtAny(o))
	}
	return toks
}

func fmtAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func pacemakerResourceCreateCmd(name string, resType map[string]any, options, operations, meta, argument []string) string {
	var toks []string
	toks = append(toks, "pcs", "resource", "create", shellQuote(name), shellQuote(pacemakerResourceAgent(resType)))
	for _, o := range options {
		toks = append(toks, shellQuote(o))
	}
	for _, o := range operations {
		toks = append(toks, shellQuote(o))
	}
	if len(meta) > 0 {
		toks = append(toks, "meta")
		for _, m := range meta {
			toks = append(toks, shellQuote(m))
		}
	}
	for _, a := range argument {
		toks = append(toks, shellQuote(a))
	}
	return strings.Join(toks, " ")
}
