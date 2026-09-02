package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSetFact implements Ansible's `set_fact` module: every argument
// becomes a fact (merged into ansible_facts / the variable scope by the
// caller). It touches nothing on the target and is never reported as
// "changed", matching Ansible.
func moduleSetFact(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return Result{Msg: "facts set", Facts: args}, nil
}
