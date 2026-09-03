package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSynchronize implements Ansible's `synchronize` module — or
// rather, documents why this port does not.
//
// Real ansible.posix.synchronize is a wrapper around `rsync` that runs
// ON THE CONTROLLER, not on the target: it shells out to a local rsync
// process that itself opens an SSH (or local-copy) connection directly
// to the target's own host/port/credentials, bypassing the normal
// module-execution path entirely (see real synchronize's own doc:
// "It is run and originates on the local host where Ansible is being
// run"). That is fundamentally incompatible with this package's
// architecture (see module.go's package doc comment): a module here
// receives only a remoteexec.Connection, whose interface is Exec/Put/
// Fetch/Remove/TempPath/Close — an abstraction deliberately opaque
// about WHAT is on the other end (local exec, SSH, WinRM all implement
// the same four-method surface). There is no host, port, username, or
// key material exposed anywhere a module function can reach; rsync
// fundamentally needs to be told both a source and destination
// endpoint's connection details to run at all, and this port's
// Connection gives a module neither the target's connection details
// nor a way to invoke a second, independent connection to run rsync
// against.
//
// A partial implementation was considered and rejected: approximating
// `mode: push` via a recursive conn.Put loop would silently drop
// nearly everything that makes synchronize worth using over `copy`
// (delete, checksum-based skip, compression, partial-transfer resume,
// permission/ownership preservation, exclude patterns) while LOOKING
// like the real module in a playbook — exactly the "silently wrong"
// outcome this package's iron rule forbids. So, matching the
// convention already established by async_status.go (and pause.go's
// no-duration form): synchronize always fails, loudly and specifically,
// rather than faking a subset that would mislead a playbook author
// into thinking they have rsync's real guarantees.
//
// Args: src, dest, mode (accepted for shape-compatibility with real
// synchronize's argspec; unused, since the module never runs).
func moduleSynchronize(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return Fail("synchronize: not implemented in this port — real ansible.posix.synchronize runs rsync " +
		"directly between the controller and the target's own connection endpoint, which this " +
		"package's remoteexec.Connection abstraction (Exec/Put/Fetch/Remove/TempPath/Close only, no " +
		"exposed host/port/credentials) has no way to reach; see moduleSynchronize's doc comment"), nil
}
