package modules

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleShutdownDefault(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -x /sbin/shutdown":                               {RC: 0},
		"/sbin/shutdown -h 0 'Shut down initiated by Ansible'": {RC: 0},
	})
	res, err := moduleShutdown(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["shutdown"] != true {
		t.Fatalf("shutdown extra = %v", res.Extra["shutdown"])
	}
}

func TestModuleShutdownSearchesPaths(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -x /sbin/shutdown":                                   {RC: 1},
		"test -x /usr/sbin/shutdown":                               {RC: 0},
		"/usr/sbin/shutdown -h 1 'Shut down initiated by Ansible'": {RC: 0},
	})
	res, err := moduleShutdown(context.Background(), conn, map[string]any{"delay": 90})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleShutdownFallsBackToSystemctl(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -x /sbin/shutdown":           {RC: 1},
		"test -x /usr/sbin/shutdown":       {RC: 1},
		"test -x /usr/local/sbin/shutdown": {RC: 1},
		"test -x /bin/systemctl":           {RC: 1},
		"test -x /usr/bin/systemctl":       {RC: 0},
		"/usr/bin/systemctl poweroff":      {RC: 0},
	})
	res, err := moduleShutdown(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["shutdown_command"] != "/usr/bin/systemctl poweroff" {
		t.Fatalf("shutdown_command = %v", res.Extra["shutdown_command"])
	}
}

func TestModuleShutdownNoneFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -x /sbin/shutdown":           {RC: 1},
		"test -x /usr/sbin/shutdown":       {RC: 1},
		"test -x /usr/local/sbin/shutdown": {RC: 1},
		"test -x /bin/systemctl":           {RC: 1},
		"test -x /usr/bin/systemctl":       {RC: 1},
	})
	res, err := moduleShutdown(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when neither shutdown nor systemctl is found")
	}
}

func TestModuleShutdownConnectionDroppedIsSuccess(t *testing.T) {
	conn := newDroppingConn()
	res, err := moduleShutdown(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want a dropped connection treated as a successful shutdown, res = %+v", res)
	}
}

// droppingConn finds /sbin/shutdown normally, then simulates the
// connection dying mid-shutdown-command, like a real successful
// shutdown would.
type droppingConn struct{ *fakeConn }

func newDroppingConn() *droppingConn {
	return &droppingConn{fakeConn: newFakeConn(map[string]remoteexec.Result{
		"test -x /sbin/shutdown": {RC: 0},
	})}
}

func (d *droppingConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	if strings.HasPrefix(cmd, "test -x") {
		return d.fakeConn.Exec(ctx, cmd, stdin)
	}
	return remoteexec.Result{}, errors.New("connection lost")
}
