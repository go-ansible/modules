package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSnapMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSnap(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleSnapChannelWithMultipleNamesRejected(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSnap(context.Background(), conn, map[string]any{
		"name": []any{"a", "b"}, "channel": "stable",
	}); err == nil {
		t.Fatal("want error: channel only valid for a single snap")
	}
}

func TestModuleSnapInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl >/dev/null 2>&1": {RC: 1},
		"snap install curl":              {RC: 0},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSnapAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl >/dev/null 2>&1": {RC: 0},
		"snap remove curl":               {RC: 0},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSnapLatestUnsupported(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"}); err == nil {
		t.Fatal("want error: snap.go passes nil for latest, state=latest is unsupported")
	}
}

func TestModuleSnapClassicFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl >/dev/null 2>&1": {RC: 1},
		"snap install --classic curl":    {RC: 0},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "classic": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapChannelFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl >/dev/null 2>&1":     {RC: 1},
		"snap install --channel=stable curl": {RC: 0},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "channel": "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapClassicAndChannelCombined(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl >/dev/null 2>&1":               {RC: 1},
		"snap install --classic --channel=stable curl": {RC: 0},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{
		"name": "curl", "classic": true, "channel": "stable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapEnabledAlreadyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl 2>/dev/null | awk 'NR==2{print $NF}'": {RC: 0, Stdout: "-\n"},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "state": "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSnapEnabledCurrentlyDisabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl 2>/dev/null | awk 'NR==2{print $NF}'": {RC: 0, Stdout: "disabled\n"},
		"snap enable curl": {RC: 0},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "state": "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapDisabledCurrentlyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl 2>/dev/null | awk 'NR==2{print $NF}'": {RC: 0, Stdout: "-\n"},
		"snap disable curl": {RC: 0},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapDisabledAlreadyDisabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl 2>/dev/null | awk 'NR==2{print $NF}'": {RC: 0, Stdout: "disabled\n"},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSnapEnabledNonzeroRCCountsAsNotDisabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap list curl 2>/dev/null | awk 'NR==2{print $NF}'": {RC: 1, Stdout: "disabled\n"},
	})
	res, err := moduleSnap(context.Background(), conn, map[string]any{"name": "curl", "state": "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: a nonzero RC from snap list means isDisabled is treated as false")
	}
}
