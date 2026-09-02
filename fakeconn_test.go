package modules

import (
	"context"
	"io"

	remoteexec "github.com/go-remoteexec/transport"
)

// fakeConn is a scripted remoteexec.Connection for unit-testing a
// module's command generation and idempotency logic without touching
// the real OS (package managers, user/group databases, systemd) — used
// for modules whose real backends need root or aren't portable to a CI
// sandbox. Modules with no such constraint are tested against a real
// remoteexec.Local connection instead (see the other _test.go files),
// which is worth more.
type fakeConn struct {
	// on maps a command's exact string to a scripted (Result, error).
	// A command not present here fails the test via t.Fatal from the
	// caller's own assertions on Commands, not from fakeConn itself —
	// missing entries return a zero Result and no error, so a bug shows
	// up as a wrong-Commands assertion rather than a silent false pass.
	on map[string]remoteexec.Result

	// Commands records every Exec call, in order, for assertions.
	Commands []string
	Stdins   []string

	closed bool
}

func newFakeConn(on map[string]remoteexec.Result) *fakeConn {
	return &fakeConn{on: on}
}

func (f *fakeConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	f.Commands = append(f.Commands, cmd)
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		f.Stdins = append(f.Stdins, string(data))
	} else {
		f.Stdins = append(f.Stdins, "")
	}
	if res, ok := f.on[cmd]; ok {
		return res, nil
	}
	return remoteexec.Result{}, nil
}

func (f *fakeConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return nil
}

func (f *fakeConn) Fetch(ctx context.Context, remotePath, localPath string) error {
	return nil
}

func (f *fakeConn) Remove(ctx context.Context, remotePath string) error {
	return nil
}

func (f *fakeConn) TempPath(base string) string {
	return "/tmp/" + base
}

func (f *fakeConn) Close() error {
	f.closed = true
	return nil
}

var _ remoteexec.Connection = (*fakeConn)(nil)
