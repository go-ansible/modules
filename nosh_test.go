package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNoshMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleNosh(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleNoshServiceNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"system-control find bogus": {RC: 1, Stderr: "not found"},
	})
	res, err := moduleNosh(context.Background(), conn, map[string]any{"name": "bogus", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a service system-control cannot find")
	}
}

func TestModuleNoshAlreadyStarted(t *testing.T) {
	path := "/var/sv/dnscache"
	statusJSON := `{"` + path + `":{"DaemontoolsEncoreState":"running"}}`
	conn := newFakeConn(map[string]remoteexec.Result{
		"system-control find dnscache":            {RC: 0, Stdout: path + "\n"},
		"system-control is-enabled " + path:       {RC: 0},
		"system-control preset --dry-run " + path: {RC: 0, Stdout: "enable\n"},
		"system-control is-loaded " + path:        {RC: 0},
		"system-control show-json " + path:        {RC: 0, Stdout: statusJSON},
	})
	res, err := moduleNosh(context.Background(), conn, map[string]any{"name": "dnscache", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Fatal("want unchanged: already running")
	}
	if res.Extra["state"] != "started" {
		t.Fatalf("state = %v", res.Extra["state"])
	}
}

func TestModuleNoshStartsStoppedService(t *testing.T) {
	path := "/var/sv/mpd"
	statusJSON := `{"` + path + `":{"DaemontoolsEncoreState":"down"}}`
	conn := newFakeConn(map[string]remoteexec.Result{
		"system-control find mpd":                 {RC: 0, Stdout: path + "\n"},
		"system-control is-enabled " + path:       {RC: 1},
		"system-control preset --dry-run " + path: {RC: 0, Stdout: "disable\n"},
		"system-control is-loaded " + path:        {RC: 0},
		"system-control show-json " + path:        {RC: 0, Stdout: statusJSON},
		"system-control start " + path:            {RC: 0},
	})
	res, err := moduleNosh(context.Background(), conn, map[string]any{"name": "mpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNoshDisable(t *testing.T) {
	path := "/var/sv/nsd"
	conn := newFakeConn(map[string]remoteexec.Result{
		"system-control find nsd":                 {RC: 0, Stdout: path + "\n"},
		"system-control is-enabled " + path:       {RC: 0}, // currently enabled
		"system-control preset --dry-run " + path: {RC: 0, Stdout: "enable\n"},
		"system-control disable " + path:          {RC: 0},
		"system-control is-loaded " + path:        {RC: 1},
	})
	res, err := moduleNosh(context.Background(), conn, map[string]any{"name": "nsd", "enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["enabled"] != false {
		t.Fatalf("enabled = %v", res.Extra["enabled"])
	}
}
