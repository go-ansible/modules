package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacemakerClusterOfflineWholeCluster(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs cluster status": {
			{RC: 0, Stdout: "Online: [ node1 node2 ]"},
			{RC: 1, Stderr: "Error: cluster is not currently running"},
		},
		"pcs cluster stop --all --wait=300": {{RC: 0}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "offline"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["out"] != "" {
		t.Fatalf("out = %v", res.Extra["out"])
	}
}

func TestModulePacemakerClusterOfflineAlreadyOffline(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs cluster status":                {{RC: 1, Stderr: "Error: cluster is not currently running"}},
		"pcs cluster stop --all --wait=300": {{RC: 1, Stderr: "Error: cluster is not currently running"}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "offline"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want the tolerated 'not currently running' error not to fail the task")
	}
	if res.Changed {
		t.Fatal("want unchanged: nothing about the status text changed")
	}
}

func TestModulePacemakerClusterOfflineWithNode(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs cluster status node1":          {{RC: 0, Stdout: "* Node node1: Online"}},
		"pcs cluster stop node1 --wait=300": {{RC: 0}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{
		"state": "offline", "name": "node1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	found := false
	for _, c := range conn.Commands {
		if c == "pcs cluster stop node1 --wait=300" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want a targeted (non --all) stop for a specific node", conn.Commands)
	}
}

func TestModulePacemakerClusterOnlineStartsWhenDown(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs cluster status": {
			{RC: 1, Stdout: ""},
			{RC: 0, Stdout: "Online: [ node1 ]"},
		},
		"pcs cluster start --all --wait=300": {{RC: 0}},
		"pcs property config":                {{RC: 0, Stdout: "maintenance-mode: false\n"}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "online"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerClusterOnlineAlreadyUpNoStart(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs cluster status":  {{RC: 0, Stdout: "Online: [ node1 ]"}},
		"pcs property config": {{RC: 0, Stdout: "maintenance-mode: false\n"}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "online"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already running, not in maintenance")
	}
	for _, c := range conn.Commands {
		if c == "pcs cluster start --all --wait=300" {
			t.Fatal("must not attempt to start an already-running cluster")
		}
	}
}

func TestModulePacemakerClusterOnlineClearsMaintenance(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs cluster status":                      {{RC: 0, Stdout: "Online: [ node1 ]"}},
		"pcs property config":                     {{RC: 0, Stdout: "maintenance-mode: true\n"}},
		"pcs property set maintenance-mode=false": {{RC: 0}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "online"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "pcs property set maintenance-mode=false" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want a maintenance-mode=false fixup", conn.Commands)
	}
	_ = res
}

func TestModulePacemakerClusterRestart(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs cluster status": {
			{RC: 0, Stdout: "Online: [ node1 ]"},
			{RC: 0, Stdout: "Online: [ node1 ] (restarted)"},
		},
		"pcs cluster stop --all --wait=300":  {{RC: 0}},
		"pcs cluster start --all --wait=300": {{RC: 0}},
		"pcs property config":                {{RC: 0, Stdout: "maintenance-mode: false\n"}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "restart"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	var gotStop, gotStart bool
	for _, c := range conn.Commands {
		if c == "pcs cluster stop --all --wait=300" {
			gotStop = true
		}
		if c == "pcs cluster start --all --wait=300" {
			gotStart = true
		}
	}
	if !gotStop || !gotStart {
		t.Fatalf("commands = %v, want both stop and start", conn.Commands)
	}
}

func TestModulePacemakerClusterMaintenance(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs property config maintenance-mode": {
			{RC: 0, Stdout: "maintenance-mode: false\n"},
			{RC: 0, Stdout: "maintenance-mode: true\n"},
		},
		"pcs property set maintenance-mode=true": {{RC: 0}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerClusterUnmaintenance(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs property config maintenance-mode": {
			{RC: 0, Stdout: "maintenance-mode: true\n"},
			{RC: 0, Stdout: "maintenance-mode: false\n"},
		},
		"pcs property set maintenance-mode=false": {{RC: 0}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "unmaintenance"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerClusterCleanup(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status": {
			{RC: 0, Stdout: "some failures"},
			{RC: 0, Stdout: ""},
		},
		"pcs resource cleanup": {{RC: 0}},
	})
	res, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerClusterCleanupWithName(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status node1":  {{RC: 0, Stdout: ""}},
		"pcs resource cleanup node1": {{RC: 0}},
	})
	_, err := modulePacemakerCluster(context.Background(), conn, map[string]any{
		"state": "cleanup", "name": "node1",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "pcs resource cleanup node1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePacemakerClusterValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePacemakerCluster(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing state")
	}
	if _, err := modulePacemakerCluster(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}

func TestPacemakerClusterRunning(t *testing.T) {
	if pacemakerClusterRunning("", "") {
		t.Fatal("empty status = not running")
	}
	if pacemakerClusterRunning("Error: cluster is not currently running", "") {
		t.Fatal("'not currently running' = not running")
	}
	if !pacemakerClusterRunning("Online: [ node1 ]", "") {
		t.Fatal("non-empty status with no node filter = running")
	}
	status := "* Node node1: Online\n* Node node2: (offline)"
	if !pacemakerClusterRunning(status, "node1") {
		t.Fatal("node1 is online")
	}
	if pacemakerClusterRunning(status, "node2") {
		t.Fatal("node2 is offline")
	}
}
