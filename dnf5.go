package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDnf5 implements (a subset of) Ansible's `dnf5` module: RHEL9+/
// Fedora's rewritten package manager. dnf5's CLI is compatible with
// classic dnf for the install/remove/update operations this port
// composes, so this is a thin wrapper around the shared dnfLike helper
// (see dnf.go) with the binary name swapped — there is no behavioral
// difference worth keeping separate at this port's level of fidelity
// (real dnf5 does differ from dnf in module-stream handling, weak-deps
// defaults, and its own set of CLI flags, none of which this port's
// simplified install/remove/upgrade composition touches).
//
// Args: name (string or []string, required); state (present|latest|
// absent, default "present").
func moduleDnf5(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return dnfLike(ctx, conn, args, "dnf5")
}
