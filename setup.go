package modules

import (
	"context"

	facts "github.com/go-ansible/facts"
	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSetup implements Ansible's `setup` module: gathers a portable
// subset of system facts from the target — what `gather_facts:` uses
// under the hood, and also what real Ansible lets a caller invoke
// directly (`ansible all -m setup`).
//
// go-ansible already has a separate github.com/go-ansible/facts
// package doing this same job for the playbook engine's own implicit
// `gather_facts:` step (see engine.go in the playbook module). This
// module delegates to that package's Gather function directly rather
// than reimplementing fact-gathering here: facts.go's go.mod depends
// only on github.com/go-remoteexec/transport (checked before adding
// this dependency), so modules -> facts is a new one-directional edge,
// not a cycle — facts does not, and structurally cannot, depend back on
// modules or playbook. This keeps the probing logic in exactly one
// place instead of forking it.
//
// Args: fact_path, filter, gather_subset (accepted for shape-
// compatibility with real ansible.builtin.setup's argument spec, but
// NOT implemented — this port's facts.Gather always collects its own
// fixed, portable subset and has no filtering, subsetting, or
// local-facts-directory support; passing any of these arguments is a
// silent no-op, not an error, since real Ansible callers frequently
// pass e.g. gather_subset even when every fact is wanted).
func moduleSetup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	gathered, err := facts.Gather(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	return Result{Msg: "gathered facts", Facts: gathered}, nil
}
