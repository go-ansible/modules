package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXenserverGuestInfo implements Ansible's `xenserver_guest_info`
// (community.general) module: a read-only module that gathers facts
// about a single XenServer VM. See xeBin's own doc comment
// (xenserver_common.go) for this port's `xe` CLI substitution, and
// xeVMFacts's own doc comment for the specific fidelity gaps in the
// returned "instance" fact tree relative to real gather_vm_facts().
//
// Args: hostname (default "localhost", aliases host/pool);
// username (default "root", aliases admin/user); password (aliases
// pass/pwd); validate_certs (accepted, no-op — see xeConnArgs's own doc
// comment); name (aliases name_label) or uuid — at least one required,
// matching real xenserver_guest_info.py's own required_one_of; name
// must resolve to exactly one VM (matching real get_object_ref()'s own
// "fails if multiple VMs with same name are found" behavior) or this
// returns Result{Failed:true}, not a Go error — a well-formed request
// the target simply can't satisfy unambiguously.
//
// Returns Extra["instance"]: the VM's fact tree (see xeVMFacts). Always
// read-only — no `xe` command that could change VM state is ever run.
func moduleXenserverGuestInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	uuid, failMsg, err := xeFindVMUUID(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}
	facts, err := xeVMFacts(ctx, conn, args, uuid)
	if err != nil {
		return Result{}, err
	}
	return Ok("gathered facts for "+uuid).WithExtra("instance", facts), nil
}
