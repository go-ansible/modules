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
	// Real's set_fact action plugin ends with
	//
	//	result['ansible_facts'] = facts
	//	return result
	//
	// and sets no msg, so `r.msg` after a real set_fact is an UNDEFINED
	// variable. This reported "facts set", which is a sentence real
	// never emits -- measured: real's key set is
	// ansible_facts,changed,failed and ours had msg on top.
	return Result{NoMsg: true, Facts: args}, nil
}
