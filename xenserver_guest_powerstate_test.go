package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXenserverGuestPowerstateMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXenserverGuestPowerstate(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name/uuid")
	}
}

func TestModuleXenserverGuestPowerstatePresentNoAction(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list name-label=myvm params=uuid --minimal": {RC: 0, Stdout: "vm-uuid-1"},
		"xe vm-param-list uuid=vm-uuid-1":                  {RC: 0, Stdout: vmParamListStdout()},
	})
	res, err := moduleXenserverGuestPowerstate(context.Background(), conn, map[string]any{"name": "myvm"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged for state=present")
	}
	for _, c := range conn.Commands {
		if c == "xe vm-start uuid=vm-uuid-1" {
			t.Fatal("state=present must not act")
		}
	}
}

func TestModuleXenserverGuestPowerstatePowerOn(t *testing.T) {
	conn := &sequencedFakeConn{
		fakeConn: newFakeConn(nil),
		script: []scriptedExec{
			{result: remoteexec.Result{RC: 0, Stdout: "vm-uuid-1"}},         // vm-list
			{result: remoteexec.Result{RC: 0, Stdout: "halted"}},            // power-state (before)
			{result: remoteexec.Result{RC: 0}},                              // vm-start
			{result: remoteexec.Result{RC: 0, Stdout: "running"}},           // power-state (after)
			{result: remoteexec.Result{RC: 0, Stdout: vmParamListStdout()}}, // vm-param-list (facts)
		},
	}
	res, err := moduleXenserverGuestPowerstate(context.Background(), conn, map[string]any{
		"name": "myvm", "state": "powered-on",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.fakeConn.Commands {
		if c == "xe vm-start uuid=vm-uuid-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want vm-start", conn.fakeConn.Commands)
	}
}

func TestModuleXenserverGuestPowerstateRestartRequiresRunning(t *testing.T) {
	conn := &sequencedFakeConn{
		fakeConn: newFakeConn(nil),
		script: []scriptedExec{
			{result: remoteexec.Result{RC: 0, Stdout: "vm-uuid-1"}}, // vm-list
			{result: remoteexec.Result{RC: 0, Stdout: "halted"}},    // power-state
		},
	}
	res, err := moduleXenserverGuestPowerstate(context.Background(), conn, map[string]any{
		"name": "myvm", "state": "restarted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed restarting a halted VM, res = %+v", res)
	}
}
