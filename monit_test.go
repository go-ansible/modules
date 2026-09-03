package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMonitStartAlreadyRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"monit status httpd": {RC: 0, Stdout: "Process 'httpd'\n  status                       running\n"},
	})
	res, err := moduleMonit(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when already running")
	}
}

func TestModuleMonitStartNotRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"monit status httpd": {RC: 0, Stdout: "Process 'httpd'\n  status                       not monitored\n"},
		"monit start httpd":  {RC: 0},
	})
	res, err := moduleMonit(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMonitStartUnknownProcess(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"monit status httpd": {RC: 1, Stderr: "Status not available -- httpd is not monitored\n"},
	})
	res, err := moduleMonit(context.Background(), conn, map[string]any{"name": "httpd", "state": "started"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed for unknown process")
	}
}

func TestModuleMonitRestartAlwaysChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"monit restart httpd": {RC: 0},
	})
	res, err := moduleMonit(context.Background(), conn, map[string]any{"name": "httpd", "state": "restarted"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMonitUnmonitored(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"monit status httpd":    {RC: 0, Stdout: "Process 'httpd'\n  status                       running\n"},
		"monit unmonitor httpd": {RC: 0},
	})
	res, err := moduleMonit(context.Background(), conn, map[string]any{"name": "httpd", "state": "unmonitored"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMonitMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMonit(context.Background(), conn, map[string]any{"name": "httpd"}); err == nil {
		t.Fatal("want error for missing state")
	}
	if _, err := moduleMonit(context.Background(), conn, map[string]any{"state": "started"}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleMonitUnknownState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMonit(context.Background(), conn, map[string]any{"name": "httpd", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}
