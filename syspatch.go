package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSyspatch implements Ansible's `syspatch` (community.general)
// module: applies (or reverts) OpenBSD binary system patches via the
// `syspatch` binary.
//
// Args: revert (string, optional — one of "one" (revert the last
// applied patch, `syspatch -r`) or "all" (revert every applied patch,
// `syspatch -R`); unset applies all available patches, `syspatch`
// with no flags).
//
// Idempotency: `syspatch -l` lists already-applied patches and
// `syspatch -c` (check) lists patches available to apply but not yet
// applied — matching real syspatch's own module, which uses exactly
// this pair of read-only listing flags to decide whether there's
// anything to do before running the real (mutating) command. Applying:
// skipped (Ok, unchanged) if `syspatch -c` reports nothing pending.
// Reverting: skipped if `syspatch -l` reports nothing applied.
//
// Return value: reboot_needed (bool) — real syspatch's own kernel
// patches require a reboot to take effect; real syspatch's own module
// derives this from syspatch's own output/exit behavior (a patch whose
// description mentions the kernel, or a specific exit condition,
// depending on syspatch version). This port takes the same
// output-text signal real syspatch's module documents: syspatch's own
// stdout containing "reboot" (its own patches print a notice like
// "Reboot required" for a kernel patch) sets reboot_needed=true, and
// otherwise it is reported false — a plain substring check on real
// syspatch's own wording rather than parsing a distinct machine-
// readable field, since syspatch has none.
//
// Simplifications vs real syspatch: real syspatch's own module
// additionally re-execs itself after applying patches to keep polling
// syspatch's progress across a possible reboot of low-level system
// libraries mid-patch; this port issues the one command and reports
// its outcome directly.
func moduleSyspatch(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	revert := argString(args, "revert", "")

	switch revert {
	case "":
		out, err := run(ctx, conn, "syspatch -c")
		if err != nil {
			return Result{}, err
		}
		if strings.TrimSpace(out) == "" {
			return Ok("no patches available").WithExtra("reboot_needed", false), nil
		}
		res, err := runStatus(ctx, conn, "syspatch")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("syspatch: " + strings.TrimSpace(res.Stderr)), nil
		}
		reboot := strings.Contains(strings.ToLower(res.Stdout), "reboot")
		return Changed("patches applied").WithExtra("reboot_needed", reboot), nil

	case "one":
		applied, err := syspatchAnyApplied(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if !applied {
			return Ok("no patches applied to revert").WithExtra("reboot_needed", false), nil
		}
		if _, err := run(ctx, conn, "syspatch -r"); err != nil {
			return Result{}, err
		}
		return Changed("last patch reverted").WithExtra("reboot_needed", false), nil

	case "all":
		applied, err := syspatchAnyApplied(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if !applied {
			return Ok("no patches applied to revert").WithExtra("reboot_needed", false), nil
		}
		if _, err := run(ctx, conn, "syspatch -R"); err != nil {
			return Result{}, err
		}
		return Changed("all patches reverted").WithExtra("reboot_needed", false), nil

	default:
		return Result{}, errArg("syspatch: revert must be one or all, got %q", revert)
	}
}

func syspatchAnyApplied(ctx context.Context, conn remoteexec.Connection) (bool, error) {
	out, err := run(ctx, conn, "syspatch -l")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
