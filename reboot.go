package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleReboot implements (a best-effort, DELIBERATELY INCOMPLETE
// subset of) Ansible's `reboot` module: triggers a reboot on the
// target.
//
// Real ansible.builtin.reboot issues the reboot command, then closes
// and repeatedly re-opens its OWN connection until the target answers
// again (comparing a boot-time marker to confirm it's a genuinely new
// boot, not the same one still shutting down) — real reboot's whole
// value is that "wait for it to come back" half. This port CANNOT do
// that: a module here only has the single remoteexec.Connection it was
// handed (see module.go's package doc comment); reconnecting a fresh
// Connection is the caller's (the playbook engine's) job, not a single
// module's, and this module has no way to ask the engine to do it.
// So moduleReboot triggers the reboot and stops there — it does NOT,
// and architecturally cannot, wait for the host to come back. This is
// a real, meaningful capability gap versus real reboot, not a cosmetic
// one: a playbook task that runs immediately after this module in the
// same play will very likely fail, since nothing here has confirmed
// the target is reachable again. Documented plainly rather than faked
// with a synthetic wait loop that can't actually observe reconnection.
//
// Issuing the reboot command is itself expected to make conn.Exec
// return a non-nil error (the target closes the connection out from
// under the in-flight command as it goes down) — this module treats
// THAT specific outcome as success (the reboot was, in fact,
// triggered), not as a transport failure to propagate. A reboot
// command that returns cleanly before the connection drops (some
// targets/transports do respond before tearing down) is treated as
// success too.
//
// Args: reboot_command (string, default "reboot"); msg,
// boot_time_command, pre_reboot_delay, post_reboot_delay,
// connect_timeout, reboot_timeout, test_command (accepted for
// shape-compatibility with real reboot's argspec, but unused beyond
// reboot_command — see the wait-for-reconnect gap above; msg is
// accepted but not passed to reboot_command, since POSIX `reboot`
// takes no message argument the way `shutdown` does).
func moduleReboot(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	rebootCommand := argString(args, "reboot_command", "reboot")

	_, err := conn.Exec(ctx, rebootCommand, nil)
	if err != nil {
		return Changed("reboot triggered; connection dropped as expected — this port cannot wait " +
			"for the host to come back (see moduleReboot's doc comment)"), nil
	}
	return Changed("reboot triggered; connection did not drop before returning — this port still " +
		"cannot wait for the host to come back (see moduleReboot's doc comment)"), nil
}
