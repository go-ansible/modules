package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestCapabilitiesBaseName(t *testing.T) {
	if got := capabilitiesBaseName("cap_net_raw+ep"); got != "cap_net_raw" {
		t.Errorf("capabilitiesBaseName = %q", got)
	}
	if got := capabilitiesBaseName("cap_sys_chroot"); got != "cap_sys_chroot" {
		t.Errorf("capabilitiesBaseName = %q", got)
	}
}

func TestModuleCapabilitiesSetNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getcap /foo 2>/dev/null":       {RC: 0, Stdout: "/foo\n"},
		"setcap cap_sys_chroot+ep /foo": {RC: 0},
	})
	res, err := moduleCapabilities(context.Background(), conn, map[string]any{
		"path": "/foo", "capability": "cap_sys_chroot+ep",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleCapabilitiesAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getcap /foo 2>/dev/null": {RC: 0, Stdout: "/foo cap_sys_chroot+ep\n"},
	})
	res, err := moduleCapabilities(context.Background(), conn, map[string]any{
		"path": "/foo", "capability": "cap_sys_chroot+ep",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want only the getcap probe, got %v", conn.Commands)
	}
}

func TestModuleCapabilitiesRemoveOneOfMany(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getcap /bar 2>/dev/null":             {RC: 0, Stdout: "/bar = cap_net_raw+ep,cap_net_bind_service+ep\n"},
		"setcap cap_net_bind_service+ep /bar": {RC: 0},
	})
	res, err := moduleCapabilities(context.Background(), conn, map[string]any{
		"path": "/bar", "capability": "cap_net_raw", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleCapabilitiesRemoveLast(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getcap /bar 2>/dev/null": {RC: 0, Stdout: "/bar cap_net_raw+ep\n"},
		"setcap -r /bar":          {RC: 0},
	})
	res, err := moduleCapabilities(context.Background(), conn, map[string]any{
		"path": "/bar", "capability": "cap_net_raw", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleCapabilitiesAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getcap /bar 2>/dev/null": {RC: 0, Stdout: "/bar\n"},
	})
	res, err := moduleCapabilities(context.Background(), conn, map[string]any{
		"path": "/bar", "capability": "cap_net_raw", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleCapabilitiesMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleCapabilities(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
	if _, err := moduleCapabilities(context.Background(), conn, map[string]any{"path": "/x"}); err == nil {
		t.Fatal("want error for missing capability")
	}
}
