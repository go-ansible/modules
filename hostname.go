package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHostname implements (a subset of) Ansible's `hostname` module:
// sets the target's hostname, idempotently (checks the current
// hostname first via the plain `hostname` command, portable across
// systemd/BSD/macOS targets alike).
//
// Args: name (string, required).
//
// Simplification: real hostname auto-detects the right strategy per
// distribution/OS (`use` picks among alpine/debian/freebsd/macos/
// redhat/systemd/... backends, and on macOS specifically it drives
// `scutil` for HostName/ComputerName/LocalHostName rather than the
// `hostname` command at all). This port always tries `hostnamectl
// set-hostname` (systemd, the modern majority) first and falls back to
// the plain `hostname` command otherwise, matching the task's
// specified two-tier strategy rather than the full per-OS matrix.
func moduleHostname(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}

	current, err := run(ctx, conn, "hostname")
	if err != nil {
		return Result{}, err
	}
	if current == name {
		return Ok(name).WithExtra("name", name), nil
	}

	if _, err := run(ctx, conn, hostnameSetCmd(name)); err != nil {
		return Result{}, err
	}
	r := Changed(name)
	r.Facts = map[string]any{"ansible_hostname": name}
	return r.WithExtra("name", name), nil
}

// hostnameSetCmd builds the hostnamectl-with-hostname-fallback
// invocation for moduleHostname, separated out so its exact shape can
// be asserted directly in tests.
func hostnameSetCmd(name string) string {
	q := shellQuote(name)
	return "if command -v hostnamectl >/dev/null 2>&1; then hostnamectl set-hostname " + q +
		"; else hostname " + q + "; fi"
}
