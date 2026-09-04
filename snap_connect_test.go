package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const snapVersionOutput = "snap    2.60\nsnapd   2.60\nseries  16\nubuntu  22.04\nkernel  5.15.0\n"

func TestModuleSnapConnectConnects(t *testing.T) {
	on := map[string][]remoteexec.Result{
		"snap version": {{RC: 0, Stdout: snapVersionOutput}},
		"snap connections": {
			{RC: 0, Stdout: "Interface  Plug            Slot   Notes\ncamera     firefox:camera  -      -\n"},
			{RC: 0, Stdout: "Interface  Plug            Slot           Notes\ncamera     firefox:camera  :camera        manual\n"},
		},
		"snap connect firefox:camera :camera": {{RC: 0}},
	}
	conn := &queueConn{on: on}
	res, err := moduleSnapConnect(context.Background(), conn, map[string]any{
		"plug": "firefox:camera", "slot": ":camera",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	version, _ := res.Extra["version"].(map[string]any)
	if version["snap"] != "2.60" {
		t.Fatalf("version = %v", res.Extra["version"])
	}
	conns, _ := res.Extra["snap_connections"].([]map[string]any)
	if len(conns) != 1 || conns[0]["slot"] != ":camera" {
		t.Fatalf("snap_connections = %v", res.Extra["snap_connections"])
	}
}

func TestModuleSnapConnectAlreadyConnected(t *testing.T) {
	on := map[string][]remoteexec.Result{
		"snap version": {{RC: 0, Stdout: snapVersionOutput}},
		"snap connections": {
			{RC: 0, Stdout: "Interface  Plug            Slot     Notes\ncamera     firefox:camera  :camera  manual\n"},
		},
	}
	conn := &queueConn{on: on}
	res, err := moduleSnapConnect(context.Background(), conn, map[string]any{
		"plug": "firefox:camera", "slot": ":camera",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
	for _, c := range conn.Commands {
		if c == "snap connect firefox:camera :camera" {
			t.Fatal("did not expect a connect call")
		}
	}
}

func TestModuleSnapConnectAutoResolvedSlot(t *testing.T) {
	on := map[string][]remoteexec.Result{
		"snap version":                {{RC: 0, Stdout: snapVersionOutput}},
		"snap connections":            {{RC: 0, Stdout: "Interface  Plug           Slot  Notes\nnetwork    my-app:network -     -\n"}},
		"snap connect my-app:network": {{RC: 0}},
	}
	conn := &queueConn{on: on}
	res, err := moduleSnapConnect(context.Background(), conn, map[string]any{"plug": "my-app:network"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapConnectDisconnect(t *testing.T) {
	on := map[string][]remoteexec.Result{
		"snap version": {{RC: 0, Stdout: snapVersionOutput}},
		"snap connections": {
			{RC: 0, Stdout: "Interface  Plug            Slot     Notes\ncamera     firefox:camera  :camera  manual\n"},
		},
		"snap disconnect firefox:camera :camera": {{RC: 0}},
	}
	conn := &queueConn{on: on}
	res, err := moduleSnapConnect(context.Background(), conn, map[string]any{
		"plug": "firefox:camera", "slot": ":camera", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapConnectDisconnectAlreadyAbsent(t *testing.T) {
	on := map[string][]remoteexec.Result{
		"snap version":     {{RC: 0, Stdout: snapVersionOutput}},
		"snap connections": {{RC: 0, Stdout: "Interface  Plug            Slot  Notes\ncamera     firefox:camera  -     -\n"}},
	}
	conn := &queueConn{on: on}
	res, err := moduleSnapConnect(context.Background(), conn, map[string]any{
		"plug": "firefox:camera", "slot": ":camera", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleSnapConnectFailure(t *testing.T) {
	on := map[string][]remoteexec.Result{
		"snap version":                        {{RC: 0, Stdout: snapVersionOutput}},
		"snap connections":                    {{RC: 0, Stdout: "Interface Plug Slot Notes\n"}},
		"snap connect firefox:camera :camera": {{RC: 1, Stderr: "no such interface"}},
	}
	conn := &queueConn{on: on}
	res, err := moduleSnapConnect(context.Background(), conn, map[string]any{
		"plug": "firefox:camera", "slot": ":camera",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}

func TestModuleSnapConnectMissingPlug(t *testing.T) {
	conn := &queueConn{}
	if _, err := moduleSnapConnect(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing plug")
	}
}

func TestModuleSnapConnectBadState(t *testing.T) {
	conn := &queueConn{}
	if _, err := moduleSnapConnect(context.Background(), conn, map[string]any{
		"plug": "x:y", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
