package modules

import (
	"context"
	"io"

	remoteexec "github.com/go-remoteexec/transport"
)

// seqConn is a scripted remoteexec.Connection like fakeConn, but each
// command name maps to a QUEUE of results consumed in order (the last
// entry repeats once the queue is drained). fakeConn's single-result-
// per-command map cannot express this batch's pacemaker modules' own
// "changed" detection, which runs the SAME status-probe command twice
// (before and after an action) and diffs the two outputs — so a test
// needs the probe to answer differently on its second call.
type seqConn struct {
	queues   map[string][]remoteexec.Result
	Commands []string
}

func newSeqConn(queues map[string][]remoteexec.Result) *seqConn {
	return &seqConn{queues: queues}
}

func (f *seqConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	f.Commands = append(f.Commands, cmd)
	q := f.queues[cmd]
	if len(q) == 0 {
		return remoteexec.Result{}, nil
	}
	res := q[0]
	if len(q) > 1 {
		f.queues[cmd] = q[1:]
	}
	return res, nil
}

func (f *seqConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return nil
}
func (f *seqConn) Fetch(ctx context.Context, remotePath, localPath string) error { return nil }
func (f *seqConn) Remove(ctx context.Context, remotePath string) error           { return nil }
func (f *seqConn) TempPath(base string) string                                   { return "/tmp/" + base }
func (f *seqConn) Close() error                                                  { return nil }

var _ remoteexec.Connection = (*seqConn)(nil)
