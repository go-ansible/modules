package modules

import (
	"context"
	"io"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleWaitForConnectionImmediateSuccess(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"true": {RC: 0},
	})
	res, err := moduleWaitForConnection(context.Background(), conn, map[string]any{"sleep": 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("Commands = %v, want exactly one attempt", conn.Commands)
	}
}

func TestModuleWaitForConnectionTimesOutFast(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"true": {RC: 1},
	})
	res, err := moduleWaitForConnection(context.Background(), conn, map[string]any{"timeout": 0, "sleep": 0})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed on timeout")
	}
}

// retryConn succeeds on Exec calls after the Nth (1-indexed) attempt,
// for exercising moduleWaitForConnection's "eventually succeeds" path
// without a real transport.
type retryConn struct {
	succeedOn int
	calls     int
}

func (c *retryConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	c.calls++
	if c.calls >= c.succeedOn {
		return remoteexec.Result{RC: 0}, nil
	}
	return remoteexec.Result{RC: 1}, nil
}
func (c *retryConn) Put(ctx context.Context, l, r string, o remoteexec.PutOptions) error { return nil }
func (c *retryConn) Fetch(ctx context.Context, r, l string) error                        { return nil }
func (c *retryConn) Remove(ctx context.Context, r string) error                          { return nil }
func (c *retryConn) TempPath(base string) string                                         { return "/tmp/" + base }
func (c *retryConn) Close() error                                                        { return nil }

var _ remoteexec.Connection = (*retryConn)(nil)

func TestModuleWaitForConnectionEventualSuccess(t *testing.T) {
	conn := &retryConn{succeedOn: 3}
	res, err := moduleWaitForConnection(context.Background(), conn, map[string]any{"sleep": 0, "timeout": 600})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if conn.calls != 3 {
		t.Fatalf("calls = %d, want 3", conn.calls)
	}
}

func TestModuleWaitForConnectionCtxCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn := newFakeConn(map[string]remoteexec.Result{"true": {RC: 1}})
	_, err := moduleWaitForConnection(ctx, conn, map[string]any{"delay": 1})
	if err == nil {
		t.Fatal("want error from canceled context during delay")
	}
}
