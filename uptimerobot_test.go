package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUptimerobotPause(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v uptimerobot": {RC: 0},
		"UPTIMEROBOT_API_KEY=12345-1234512345 uptimerobot monitors get 12345 --json":           {RC: 0, Stdout: `{"id":12345,"status":2}`},
		"UPTIMEROBOT_API_KEY=12345-1234512345 uptimerobot monitors bulk pause 12345 --confirm": {RC: 0},
	})
	res, err := moduleUptimerobot(context.Background(), conn, map[string]any{
		"monitorid": "12345", "apikey": "12345-1234512345", "state": "paused",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged (real quirk), not failed; res = %+v", res)
	}
}

func TestModuleUptimerobotStart(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v uptimerobot": {RC: 0},
		"UPTIMEROBOT_API_KEY=12345-1234512345 uptimerobot monitors get 12345 --json":           {RC: 0, Stdout: `{"id":12345,"status":0}`},
		"UPTIMEROBOT_API_KEY=12345-1234512345 uptimerobot monitors bulk start 12345 --confirm": {RC: 0},
	})
	res, err := moduleUptimerobot(context.Background(), conn, map[string]any{
		"monitorid": "12345", "apikey": "12345-1234512345", "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged, not failed; res = %+v", res)
	}
}

func TestModuleUptimerobotMonitorNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v uptimerobot": {RC: 0},
		"UPTIMEROBOT_API_KEY=12345-1234512345 uptimerobot monitors get 999 --json": {RC: 6, Stderr: `{"error":{"code":"HTTP_404","message":"Monitor not found"}}`},
	})
	res, err := moduleUptimerobot(context.Background(), conn, map[string]any{
		"monitorid": "999", "apikey": "12345-1234512345", "state": "paused",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleUptimerobotInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleUptimerobot(context.Background(), conn, map[string]any{
		"monitorid": "12345", "apikey": "x", "state": "bogus",
	})
	if err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleUptimerobotMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v uptimerobot": {RC: 1},
	})
	res, err := moduleUptimerobot(context.Background(), conn, map[string]any{
		"monitorid": "12345", "apikey": "x", "state": "paused",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
