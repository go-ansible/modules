package modules

import (
	"strings"

	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacemakerStonith implements Ansible's `pacemaker_stonith` module:
// manages a Pacemaker STONITH (fencing) resource's state via the `pcs`
// CLI (matching real pacemaker_stonith's own module_utils, _pacemaker,
// which wraps `pcs` — never `crm` — the same helper this package's
// pacemaker_cluster.go/pacemaker_resource.go/pacemaker_info.go already
// use; read those three files' own doc comments and this file reuses
// their pacemakerStatusText/pacemakerStep helpers directly).
//
// Args: state (present|absent|enabled|disabled, default "present");
// name (required) — the STONITH resource's own id; stonith_type
// (string) — required when state=present, the fence agent (e.g.
// fence_virt); stonith_options ([]string, default []) — raw
// "key=value" tokens appended after the type; stonith_operations
// ([]dict{operation_action, operation_options}, default []) — each
// entry becomes "op <action> <option>..." (note the real module's own
// plural `operation_options` sub-key, unlike pacemaker_resource's
// singular `operation_option` — this port matches each module's own
// real key names exactly rather than sharing one shape); stonith_metas
// ([]string) — matching real pacemaker_stonith's own _pacemaker.py
// arg-formatting (`cmd_runner_fmt.stack(as_opt_val)("meta")`), each
// entry becomes its OWN "meta <item>" pair (e.g. metas=[a,b] renders
// as "meta a meta b" — NOT a single "meta" keyword followed by every
// item, unlike this file's sibling resource_meta handling elsewhere in
// this package); stonith_argument (dict{argument_action:
// group|before|after, argument_options}) — becomes "--group
// <option>..." for argument_action=group, or "<argument_action>
// <option>..." (no "--" prefix) for before/after, matching real
// _pacemaker.py's fmt_resource_argument exactly; agent_validation
// (bool, default false) — appends `--agent-validation` when true;
// wait (int, default 300) — accepted, but this port does NOT poll for
// the STONITH resource to reach "Started" afterwards, for the same
// reason pacemaker_resource.go's own `wait` doc comment gives (a poll
// loop has no real cluster to converge against under a fakeConn, and
// would make this module's own tests slow, infinite, or meaningless);
// stored nowhere observable, matching pacemaker_resource.go's own
// choice to surface it via Extra rather than silently drop it.
//
// Changed is determined the same way real pacemaker_stonith's own
// StateModuleHelper determines it: by diffing `pcs stonith status
// [name]` text captured before and after the action (Extra["previous_value"]
// and Extra["value"], matching real pacemaker_stonith's own identically
// named return values) — not by reasoning about which individual
// command ran.
//
// Each state's own tolerated "this error means nothing actually needs
// to change" substring matches real pacemaker_stonith's own
// _process_command_output call sites exactly: "does not exist" for
// absent, "already exists" for present, "Starting" for enabled,
// "Stopped" for disabled.
func modulePacemakerStonith(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "enabled", "disabled":
	default:
		return Result{}, errArg("pacemaker_stonith: state must be one of present, absent, enabled, disabled, got %q", state)
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	if state == "present" && argString(args, "stonith_type", "") == "" {
		return Result{}, errArg("pacemaker_stonith: stonith_type is required when state is present")
	}

	getCmd := "pcs stonith status " + shellQuote(name)
	before, err := pacemakerStatusText(ctx, conn, getCmd)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		cmd := pacemakerStonithCreateCmd(name, argString(args, "stonith_type", ""),
			argStringList(args, "stonith_options"), stonithOperations(args["stonith_operations"]),
			argStringList(args, "stonith_metas"), stonithArgument(args["stonith_argument"]),
			argBool(args, "agent_validation", false))
		if res, err := pacemakerStep(ctx, conn, cmd, "already exists"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "absent":
		cmd := "pcs stonith remove " + shellQuote(name)
		if res, err := pacemakerStep(ctx, conn, cmd, "does not exist"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "enabled":
		if res, err := pacemakerStep(ctx, conn, "pcs stonith enable "+shellQuote(name), "Starting"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "disabled":
		if res, err := pacemakerStep(ctx, conn, "pcs stonith disable "+shellQuote(name), "Stopped"); err != nil {
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
	res = res.WithExtra("previous_value", nilIfEmpty(before)).WithExtra("value", nilIfEmpty(after))
	return res, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// stonithOperations mirrors pacemakerResourceOperations but reads the
// real pacemaker_stonith module's own plural `operation_options`
// sub-key (pacemaker_resource's own `resource_operation` uses the
// singular `operation_option` instead — see this file's own doc
// comment on why these aren't shared).
func stonithOperations(v any) []string {
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
		opts, _ := m["operation_options"].([]any)
		for _, o := range opts {
			toks = append(toks, fmtAny(o))
		}
	}
	return toks
}

// stonithArgument mirrors pacemakerResourceArgument but reads the real
// pacemaker_stonith module's own plural `argument_options` sub-key.
func stonithArgument(v any) []string {
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
	opts, _ := m["argument_options"].([]any)
	for _, o := range opts {
		toks = append(toks, fmtAny(o))
	}
	return toks
}

func pacemakerStonithCreateCmd(name, stonithType string, options, operations, metas, argument []string, agentValidation bool) string {
	var toks []string
	toks = append(toks, "pcs", "stonith", "create", shellQuote(name), shellQuote(stonithType))
	for _, o := range options {
		toks = append(toks, shellQuote(o))
	}
	for _, o := range operations {
		toks = append(toks, shellQuote(o))
	}
	// Each meta entry gets its OWN "meta <item>" pair — see this file's
	// own doc comment.
	for _, m := range metas {
		toks = append(toks, "meta", shellQuote(m))
	}
	for _, a := range argument {
		toks = append(toks, shellQuote(a))
	}
	if agentValidation {
		toks = append(toks, "--agent-validation")
	}
	return strings.Join(toks, " ")
}
