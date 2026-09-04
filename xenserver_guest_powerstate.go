package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXenserverGuestPowerstate implements Ansible's
// `xenserver_guest_powerstate` (community.general) module: manages a
// single XenServer VM's power state specifically — start/stop/restart/
// suspend the VM itself, or gracefully shut down/reboot the guest OS
// running inside it — narrower in scope than xenserver_guest.go's own
// full lifecycle management (create/reconfigure/destroy), matching real
// xenserver_guest_powerstate.py's own documented scope exactly: it can
// only ever act on a VM that already exists (see xeFindVMUUID — a
// missing VM is Result{Failed:true}, never created). See xeBin's own
// doc comment (xenserver_common.go) for this port's `xe` CLI
// substitution.
//
// Args: hostname/username/password/validate_certs — see
// xeConnArgs's own doc comment; name (aliases name_label) or uuid — at
// least one required; state (default "present") — one of powered-on|
// powered-off|restarted|shutdown-guest|reboot-guest|suspended|present,
// hyphens/underscores stripped before matching xeSetPowerState's own
// normalized vocabulary; state=present takes no action at all (matching
// real xenserver_guest_powerstate.py's own "just checked for existence
// and facts are returned"); every other state is idempotent against the
// VM's own current power state, with restarted/suspended/
// shutdown-guest/reboot-guest additionally requiring a specific current
// state (running, or paused for restarted) — see xeSetPowerState's own
// doc comment for the exact preconditions and their Result{Failed:true}
// messages, both copied from real set_vm_power_state()'s own
// module.fail_json calls; wait_for_ip_address (bool, default false) —
// see xeWaitForIP's own doc comment for this port's bounded-wait
// deviation from real wait_for_vm_ip_address()'s own true indefinite
// wait; state_change_timeout (int, default 0) — seconds, forwarded to
// xeWaitForIP; unlike real xenserver_guest_powerstate.py this port does
// not use this timeout to bound the power-transition command itself
// (real set_vm_power_state() only applies it to the guest-cooperative
// shutdown-guest/reboot-guest transitions, via an async XAPI task poll
// this port's synchronous `xe` invocation has no equivalent bounded-wait
// hook for — a shutdown-guest/reboot-guest task can therefore block
// this port for as long as `xe vm-shutdown`/`xe vm-reboot` themselves
// take to return, rather than failing after state_change_timeout
// seconds the way real set_vm_power_state()'s own async-task-poll path
// does).
//
// Returns Extra["instance"]: the VM's post-transition fact tree (see
// xeVMFacts), always populated — matching real
// xenserver_guest_powerstate.py's own "returned: always" instance.
func moduleXenserverGuestPowerstate(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	uuid, failMsg, err := xeFindVMUUID(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	state := argString(args, "state", "present")
	changed := false

	if state != "present" {
		normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(state))
		timeout := argInt(args, "state_change_timeout", 0)
		c, _, transitionFailMsg, err := xeSetPowerState(ctx, conn, args, uuid, normalized, timeout)
		if err != nil {
			return Result{}, err
		}
		if transitionFailMsg != "" {
			facts, factsErr := xeVMFacts(ctx, conn, args, uuid)
			res := Fail(transitionFailMsg)
			if factsErr == nil {
				res = res.WithExtra("instance", facts)
			}
			return res, nil
		}
		changed = c
	}

	if argBool(args, "wait_for_ip_address", false) {
		if _, err := xeWaitForIP(ctx, conn, args, uuid, argInt(args, "state_change_timeout", 0)); err != nil {
			return Result{}, err
		}
	}

	facts, err := xeVMFacts(ctx, conn, args, uuid)
	if err != nil {
		return Result{}, err
	}
	res := Result{Changed: changed}
	res = res.WithExtra("instance", facts)
	return res, nil
}
