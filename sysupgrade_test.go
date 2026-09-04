package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSysupgradeReleaseUpgradeAvailable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/usr/sbin/sysupgrade -r -n": {RC: 0, Stdout: "Upgrade on next reboot\n"},
	})
	res, err := moduleSysupgrade(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSysupgradeAlreadyLatestSnapshot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/usr/sbin/sysupgrade -s -n": {RC: 0, Stdout: "Already on latest snapshot.\n"},
	})
	res, err := moduleSysupgrade(context.Background(), conn, map[string]any{"snapshot": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSysupgradeSnapshotForceAndInstallurl(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/usr/sbin/sysupgrade -s -f -n https://cloudflare.cdn.openbsd.org/pub/OpenBSD": {RC: 0, Stdout: "Upgrade on next reboot\n"},
	})
	res, err := moduleSysupgrade(context.Background(), conn, map[string]any{
		"snapshot": true, "force": true, "installurl": "https://cloudflare.cdn.openbsd.org/pub/OpenBSD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSysupgradeFetchOnlyFalse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/usr/sbin/sysupgrade -r": {RC: 0, Stdout: "rebooting...\n"},
	})
	res, err := moduleSysupgrade(context.Background(), conn, map[string]any{"fetch_only": false})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: no recognized phrase in stdout")
	}
}

func TestModuleSysupgradeFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/usr/sbin/sysupgrade -r -n": {RC: 1, Stderr: "boom"},
	})
	res, err := moduleSysupgrade(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}
