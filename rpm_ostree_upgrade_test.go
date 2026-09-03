package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRpmOstreeUpgradeAvailable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree upgrade": {RC: 0, Stdout: "Staging deployment...done\nUpgraded:\n  kernel 5.0 -> 5.1\n"},
	})
	res, err := moduleRpmOstreeUpgrade(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRpmOstreeUpgradeNoneAvailable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree upgrade": {RC: 0, Stdout: "No upgrade available."},
	})
	res, err := moduleRpmOstreeUpgrade(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRpmOstreeUpgradeFlags(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree upgrade --allow-downgrade --cache-only --os=edge --peer": {RC: 0, Stdout: "No upgrade available."},
	})
	res, err := moduleRpmOstreeUpgrade(context.Background(), conn, map[string]any{
		"allow_downgrade": true, "cache_only": true, "os": "edge", "peer": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRpmOstreeUpgradeFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm-ostree upgrade": {RC: 1, Stderr: "error: could not connect"},
	})
	res, err := moduleRpmOstreeUpgrade(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}
