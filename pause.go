package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePause implements (a subset of) Ansible's `pause` module: pauses
// for a given duration.
//
// Real ansible.builtin.pause runs entirely on the control node (it's a
// pure action-plugin module with no target-side component at all) and,
// when neither `seconds` nor `minutes` is given, prompts interactively
// on the controller's own terminal and blocks until the user presses
// Enter (or Ctrl-C). This port has no interactive control-node
// mechanism to hook into — a module here is a Go function returning a
// Result, not a live process attached to a terminal — so the
// no-duration form cannot be honestly implemented: rather than hang
// forever waiting for input that can never arrive in this
// architecture, or silently return immediately (misrepresenting a real
// prompt-and-wait as a no-op), modulePause fails cleanly when neither
// seconds nor minutes is given.
//
// Args: seconds (int, optional); minutes (int, optional) — when both
// are given, they are summed (matching real pause's own documented
// behavior of combining them); prompt, echo (accepted for
// shape-compatibility with real pause's argspec, but unused — they
// only affect the interactive-prompt form this port doesn't support).
//
// The sleep itself is delegated to `sleep N` on the TARGET via
// conn.Exec, not a control-node time.Sleep — unlike real pause (which
// sleeps on the controller and touches the target not at all), this is
// an architectural deviation forced by module.go's Func signature
// having no way to block the engine directly; the observable delay is
// the same either way.
func modulePause(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	seconds := argInt(args, "seconds", 0)
	minutes := argInt(args, "minutes", 0)
	total := seconds + minutes*60

	if total <= 0 {
		return Fail("pause: neither seconds nor minutes was given; this port has no interactive " +
			"controller prompt to wait on, so an unbounded pause is refused rather than hanging forever"), nil
	}

	if _, err := run(ctx, conn, fmt.Sprintf("sleep %d", total)); err != nil {
		return Result{}, err
	}
	return Ok(fmt.Sprintf("paused for %d seconds", total)), nil
}
