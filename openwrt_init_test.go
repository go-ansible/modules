package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOpenwrtInitStart(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/init.d/httpd": {RC: 0},
		"/etc/init.d/httpd running": {RC: 1},
		"/etc/init.d/httpd start":   {RC: 0},
	})
	res, err := moduleOpenwrtInit(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleOpenwrtInitStopAlreadyStopped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/init.d/cron": {RC: 0},
		"/etc/init.d/cron running": {RC: 1},
	})
	res, err := moduleOpenwrtInit(context.Background(), conn, map[string]any{"name": "cron", "state": "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleOpenwrtInitReload(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/init.d/httpd": {RC: 0},
		"/etc/init.d/httpd reload":  {RC: 0},
	})
	res, err := moduleOpenwrtInit(context.Background(), conn, map[string]any{"name": "httpd", "state": "reloaded"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

// TestModuleOpenwrtInitEnableReverifiesAfterToggle exercises real
// openwrt_init.py's own documented quirk: it ignores the exit code of
// the `enable`/`disable` command itself (which can be non-zero on a
// successful enable, per its own comment) and instead re-runs the
// `enabled` query afterward to confirm the change actually stuck. This
// port's fakeConn scripts a FIXED result per exact command string, so
// it cannot simulate the target's state actually flipping between the
// two identical `enabled` queries; that makes this the one scenario a
// static fake CAN exercise end-to-end: the recheck disagreeing with
// what was requested, which must surface as a Fail (not be silently
// swallowed) — proving the reverification step is real and wired up,
// not skipped.
func TestModuleOpenwrtInitEnableReverifiesAfterToggle(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/init.d/httpd": {RC: 0},
		"/etc/init.d/httpd enabled": {RC: 1},
		"/etc/init.d/httpd enable":  {RC: 0},
	})
	res, err := moduleOpenwrtInit(context.Background(), conn, map[string]any{"name": "httpd", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed: the post-toggle 'enabled' recheck still disagrees, res = %+v", res)
	}
	count := 0
	for _, c := range conn.Commands {
		if c == "/etc/init.d/httpd enabled" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("want the 'enabled' query run twice (before and after the toggle), got %d: %v", count, conn.Commands)
	}
	if len(conn.Commands) != 4 {
		t.Fatalf("want 4 commands total (exists, enabled, enable, enabled), got %v", conn.Commands)
	}
}

func TestModuleOpenwrtInitEnableAlreadyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/init.d/httpd": {RC: 0},
		"/etc/init.d/httpd enabled": {RC: 0},
	})
	res, err := moduleOpenwrtInit(context.Background(), conn, map[string]any{"name": "httpd", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged/not-failed, res = %+v", res)
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("want no enable command run when already enabled, got %v", conn.Commands)
	}
}

func TestModuleOpenwrtInitServiceMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/init.d/nope": {RC: 1},
	})
	res, err := moduleOpenwrtInit(context.Background(), conn, map[string]any{"name": "nope", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: service does not exist")
	}
}

func TestModuleOpenwrtInitMissingStateAndEnabled(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOpenwrtInit(context.Background(), conn, map[string]any{"name": "httpd"}); err == nil {
		t.Fatal("want error: one of state or enabled required")
	}
}
