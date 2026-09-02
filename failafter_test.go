package modules

import (
	"context"
	"errors"
	"io"

	remoteexec "github.com/go-remoteexec/transport"
)

// failAfterConn succeeds (with a zero Result) for the first N Exec
// calls, then fails every call after that — for exercising a module's
// "a later step failed" branches (e.g. chmod after a successful write)
// without needing to fail the very first call.
type failAfterConn struct {
	n     int
	calls int
}

func (f *failAfterConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	f.calls++
	if f.calls > f.n {
		return remoteexec.Result{}, errors.New("boom")
	}
	return remoteexec.Result{RC: 0}, nil
}

func (f *failAfterConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	f.calls++
	if f.calls > f.n {
		return errors.New("boom")
	}
	return nil
}

func (f *failAfterConn) Fetch(ctx context.Context, remotePath, localPath string) error {
	return errors.New("not implemented in failAfterConn")
}

func (f *failAfterConn) Remove(ctx context.Context, remotePath string) error { return nil }
func (f *failAfterConn) TempPath(base string) string                         { return "/tmp/" + base }
func (f *failAfterConn) Close() error                                        { return nil }

var _ remoteexec.Connection = (*failAfterConn)(nil)
