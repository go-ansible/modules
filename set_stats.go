package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSetStats implements (a subset of) Ansible's `set_stats` module:
// accumulates custom stats into the current run.
//
// Real ansible.builtin.set_stats works by handing `data` to the
// engine's core stats accumulator, which merges (or replaces, per
// `aggregate`) it into a running total that's visible at the end of the
// whole play/run — a play-wide, cross-task accumulator this package's
// Result type has no equivalent of (Result carries one task's own
// outcome, not shared run-wide state; see module.go's Result doc
// comment). Implementing real accumulation would mean either adding
// mutable global state to this package (a module.go-level change well
// beyond what one module should decide) or having the playbook engine
// notice `Extra["set_stats"]` and thread it through itself — neither of
// which this batch's scope covers.
//
// So this port stores `data` in Extra["set_stats"] and stops there:
// nothing currently reads or aggregates it across tasks. A caller
// wanting play-wide stats today has to collect each task's
// Extra["set_stats"] itself. This is a real, documented limitation, not
// full parity with real set_stats — set_stats here behaves like a
// slightly fancier debug module, not an accumulator.
//
// Args: data (map[string]any, required); per_host, aggregate (bool,
// accepted for shape-compatibility with real set_stats' argspec, but
// unused — both only matter to the accumulation this port doesn't do).
func moduleSetStats(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	v, ok := args["data"]
	if !ok {
		return Result{}, errArg("set_stats: missing required argument: data")
	}
	data, ok := v.(map[string]any)
	if !ok {
		return Result{}, errArg("set_stats: data must be a dictionary, got %T", v)
	}
	return Ok("stats recorded (not aggregated across tasks by this port; see moduleSetStats's doc comment)").
		WithExtra("set_stats", data), nil
}
