package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleBootcManage implements Ansible's `bootc_manage` module
// (community.general): switches a `bootc`-managed system to a
// different container image, or upgrades it to the latest version of
// its current image, via `bootc switch`/`bootc upgrade`.
//
// Args: state (switch|latest, required); image (string, required when
// state=switch) — the image reference to switch to.
//
// state=switch runs `bootc switch <image> --retain` (real bootc_manage
// always passes `--retain`, keeping the previous deployment rather
// than pruning it); state=latest runs `bootc upgrade`. Neither probes
// for idempotency beforehand — matching real bootc_manage.py's own
// `check_mode` attribute (support: none, not even listed in its own
// ATTRIBUTES) exactly: the command is simply run, and its OUTCOME is
// read from its own stdout: "Queued for next boot: " means Changed;
// "No changes in " or "Image specification is unchanged." means Ok
// (no actual change was queued). A non-zero exit is an error (real
// bootc_manage.py uses `run_command(cmd, check_rc=True)`, i.e. any
// exec failure it treats as the module's own execution having gone
// wrong, not a well-formed "request can't be satisfied" — matching
// this port's own error-vs-Fail convention). Neither state reboots the
// system to apply the change — matching real bootc_manage's own
// documented note to use `ansible.builtin.reboot` separately.
func moduleBootcManage(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	var cmd string
	switch state {
	case "switch":
		image, ierr := requireString(args, "image")
		if ierr != nil {
			return Result{}, errArg("bootc_manage: image is required when state=switch")
		}
		cmd = "bootc switch " + shellQuote(image) + " --retain"
	case "latest":
		cmd = "bootc upgrade"
	default:
		return Result{}, errArg("bootc_manage: state must be switch or latest, got %q", state)
	}

	out, err := run(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	switch {
	case strings.Contains(out, "Queued for next boot: "):
		return Changed(strings.TrimSpace(out)), nil
	case strings.Contains(out, "No changes in ") || strings.Contains(out, "Image specification is unchanged."):
		return Ok(strings.TrimSpace(out)), nil
	default:
		return Fail("ERROR: Command execution failed."), nil
	}
}
