package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRhelFacts implements Ansible's `rhel_facts` module: a
// compatibility layer that sets the `pkg_mgr` fact to
// "ansible.posix.rhel_facts", so that a subsequent
// ansible.builtin.package task dispatches to rhel_rpm_ostree.go instead
// of the host's normal package manager module — exactly what real
// ansible.posix.rhel_facts does (it inspects nothing on the target
// itself; it is meant to be listed as one of `ansible_facts_modules`,
// run after `setup`, on hosts already known to be RHEL-for-Edge/
// rpm-ostree-based — see real rhel_facts' own example).
//
// Args: none.
//
// This module takes no arguments and never touches the target at all
// — it is a pure fact assignment, matching real rhel_facts exactly (it
// has no options either).
func moduleRhelFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return Result{Facts: map[string]any{"pkg_mgr": "ansible.posix.rhel_facts"}}, nil
}
