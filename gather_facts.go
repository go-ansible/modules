package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGatherFacts implements Ansible's `gather_facts` module: a thin
// wrapper that calls the `setup` module. Real ansible.builtin.gather_facts
// exists mostly as its own callable name for the engine's implicit
// facts-collection step (and, per its argspec, a `parallel` toggle for
// running multiple fact modules concurrently) — it delegates the actual
// probing to setup (or whatever module(s) ansible_facts_modules names).
// This port has exactly one fact-gathering implementation (moduleSetup),
// so gather_facts is a pure delegation to it.
//
// Args: parallel (accepted, silently ignored — meaningless with a
// single fact-gathering module and one Connection); every other
// argument is passed straight through to moduleSetup.
func moduleGatherFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return moduleSetup(ctx, conn, args)
}
