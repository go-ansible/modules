package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleTimezoneTimedatectlAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v timedatectl >/dev/null 2>&1": {RC: 0},
		"timedatectl show -p Timezone --value":   {RC: 0, Stdout: "Asia/Tokyo"},
	})
	res, err := moduleTimezone(context.Background(), conn, map[string]any{"name": "Asia/Tokyo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleTimezoneTimedatectlChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v timedatectl >/dev/null 2>&1": {RC: 0},
		"timedatectl show -p Timezone --value":   {RC: 0, Stdout: "UTC"},
	})
	res, err := moduleTimezone(context.Background(), conn, map[string]any{"name": "Asia/Tokyo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "timedatectl set-timezone Asia/Tokyo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleTimezoneHwclockLocal(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v timedatectl >/dev/null 2>&1": {RC: 0},
		"timedatectl show -p LocalRTC --value":   {RC: 0, Stdout: "no"},
	})
	res, err := moduleTimezone(context.Background(), conn, map[string]any{"hwclock": "local"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "timedatectl set-local-rtc true" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleTimezoneDebianFallback(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v timedatectl >/dev/null 2>&1": {RC: 1},
		"cat /etc/timezone 2>/dev/null":          {RC: 0, Stdout: "UTC\n"},
	})
	res, err := moduleTimezone(context.Background(), conn, map[string]any{"name": "Asia/Tokyo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "ln -sf /usr/share/zoneinfo/Asia/Tokyo /etc/localtime" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleTimezoneHwclockWithoutTimedatectlFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v timedatectl >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleTimezone(context.Background(), conn, map[string]any{"hwclock": "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: hwclock without timedatectl is not implemented")
	}
}

func TestModuleTimezoneMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleTimezone(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: at least one of name or hwclock required")
	}
}

func TestModuleTimezoneInvalidHwclock(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleTimezone(context.Background(), conn, map[string]any{"hwclock": "bogus"}); err == nil {
		t.Fatal("want error for invalid hwclock")
	}
}
