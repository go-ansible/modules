package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSolarisZoneMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSolarisZone(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleSolarisZoneInvalidName(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{"name": "bad/name"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed for an invalid zone name, got %+v", res)
	}
}

func TestModuleSolarisZoneInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSolarisZone(context.Background(), conn, map[string]any{
		"name": "myzone", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleSolarisZoneRequiresSolaris(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r": {RC: 0, Stdout: "Linux 6.1\n"},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{"name": "myzone"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed on a non-Solaris target, got %+v", res)
	}
}

func TestModuleSolarisZoneRequiresSolaris10Plus(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r": {RC: 0, Stdout: "SunOS 5.9\n"},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{"name": "myzone"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed on Solaris < 10, got %+v", res)
	}
}

func TestModuleSolarisZonePresentAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r":            {RC: 0, Stdout: "SunOS 5.11\n"},
		"zoneadm -z myzone list": {RC: 0, Stdout: "myzone\n"},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{
		"name": "myzone", "state": "present", "path": "/zones/myzone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: zone already exists")
	}
}

func TestModuleSolarisZonePresentConfiguresAndInstallsNewZone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r":            {RC: 0, Stdout: "SunOS 5.11\n"},
		"zoneadm -z myzone list": {RC: 1},
		"zonecfg -z myzone -f /tmp/solaris_zone-myzone.zonecfg": {RC: 0},
		"zoneadm -z myzone install":                             {RC: 0},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{
		"name": "myzone", "state": "present", "path": "/zones/myzone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, got %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "zoneadm -z myzone install" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected install command among %v", conn.Commands)
	}
}

func TestModuleSolarisZonePresentMissingPath(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r":            {RC: 0, Stdout: "SunOS 5.11\n"},
		"zoneadm -z myzone list": {RC: 1},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{
		"name": "myzone", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed: path is required to configure a new zone, got %+v", res)
	}
}

func TestModuleSolarisZoneStoppedDoesNotExist(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r":            {RC: 0, Stdout: "SunOS 5.11\n"},
		"zoneadm -z myzone list": {RC: 1},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{
		"name": "myzone", "state": "stopped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed: zone does not exist, got %+v", res)
	}
}

func TestModuleSolarisZoneAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r":            {RC: 0, Stdout: "SunOS 5.11\n"},
		"zoneadm -z myzone list": {RC: 1},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{
		"name": "myzone", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want a no-op ok, got %+v", res)
	}
}

func TestModuleSolarisZoneConfiguredAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r":            {RC: 0, Stdout: "SunOS 5.11\n"},
		"zoneadm -z myzone list": {RC: 0, Stdout: "myzone\n"},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{
		"name": "myzone", "state": "configured", "path": "/zones/myzone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: zone already exists")
	}
}

// TestModuleSolarisZoneAttachedQuirkOnNonexistentZone verifies this
// port's documented, deliberate replication of real solaris_zone's own
// state_attached() quirk: a nonexistent zone still ends up reporting
// "zone already attached" because there is no early return after the
// "zone does not exist" message.
func TestModuleSolarisZoneAttachedQuirkOnNonexistentZone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s -r":               {RC: 0, Stdout: "SunOS 5.11\n"},
		"zoneadm -z myzone list":    {RC: 1},
		"zoneadm -z myzone list -p": {RC: 1},
	})
	res, err := moduleSolarisZone(context.Background(), conn, map[string]any{
		"name": "myzone", "state": "attached",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged, matching real solaris_zone's own quirky fallthrough")
	}
}
