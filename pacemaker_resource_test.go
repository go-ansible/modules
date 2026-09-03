package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacemakerResourcePresentCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status virtual-ip": {
			{RC: 1, Stdout: ""},
			{RC: 0, Stdout: "virtual-ip (ocf::heartbeat:IPaddr2): Started"},
		},
		"pcs resource create virtual-ip IPaddr2 ip=192.168.2.1 op monitor interval=20 --group master": {{RC: 0}},
		"pcs property config": {{RC: 0, Stdout: "maintenance-mode: false\n"}},
	})
	res, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "present",
		"name":  "virtual-ip",
		"resource_type": map[string]any{
			"resource_name": "IPaddr2",
		},
		"resource_option": []any{"ip=192.168.2.1"},
		"resource_operation": []any{
			map[string]any{"operation_action": "monitor", "operation_option": []any{"interval=20"}},
		},
		"resource_argument": map[string]any{
			"argument_action": "group",
			"argument_option": []any{"master"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["cluster_resources"] != "virtual-ip (ocf::heartbeat:IPaddr2): Started" {
		t.Fatalf("cluster_resources = %v", res.Extra["cluster_resources"])
	}
}

func TestModulePacemakerResourcePresentWithFullAgent(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status vip":                       {{RC: 0, Stdout: ""}},
		"pcs resource create vip ocf:heartbeat:IPaddr2": {{RC: 0}},
		"pcs property config":                           {{RC: 0, Stdout: "maintenance-mode: false\n"}},
	})
	_, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "present",
		"name":  "vip",
		"resource_type": map[string]any{
			"resource_standard": "ocf", "resource_provider": "heartbeat", "resource_name": "IPaddr2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestModulePacemakerResourcePresentAlreadyExistsTolerated(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status vip": {{RC: 0, Stdout: "vip: Started"}},
		"pcs resource create vip IPaddr2": {
			{RC: 1, Stderr: "Error: 'vip' already exists"},
		},
		"pcs property config": {{RC: 0, Stdout: "maintenance-mode: false\n"}},
	})
	res, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "present",
		"name":  "vip",
		"resource_type": map[string]any{
			"resource_name": "IPaddr2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want the tolerated 'already exists' error not to fail the task")
	}
}

func TestModulePacemakerResourceAbsent(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status vip": {
			{RC: 0, Stdout: "vip: Started"},
			{RC: 1, Stdout: ""},
		},
		"pcs resource remove vip": {{RC: 0}},
		"pcs property config":     {{RC: 0, Stdout: "maintenance-mode: false\n"}},
	})
	res, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "absent", "name": "vip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerResourceAbsentForcedByMaintenance(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status vip":         {{RC: 0, Stdout: "vip: Started"}},
		"pcs property config":             {{RC: 0, Stdout: "maintenance-mode: true\n"}},
		"pcs resource remove vip --force": {{RC: 0}},
	})
	_, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "absent", "name": "vip",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "pcs resource remove vip --force" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want --force when the cluster is in maintenance mode", conn.Commands)
	}
}

func TestModulePacemakerResourceEnabled(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status vip": {
			{RC: 0, Stdout: "vip: Stopped"},
			{RC: 0, Stdout: "vip: Started"},
		},
		"pcs resource enable vip": {{RC: 0}},
	})
	res, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "enabled", "name": "vip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerResourceDisabled(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status vip": {
			{RC: 0, Stdout: "vip: Started"},
			{RC: 0, Stdout: "vip: Stopped"},
		},
		"pcs resource disable vip": {{RC: 0}},
	})
	res, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "disabled", "name": "vip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerResourceCloned(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status vip": {
			{RC: 0, Stdout: "vip: Started"},
			{RC: 0, Stdout: "vip-clone: Started"},
		},
		"pcs resource clone vip meta clone-max=3": {{RC: 0}},
		"pcs property config":                     {{RC: 0, Stdout: "maintenance-mode: false\n"}},
	})
	res, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "cloned", "name": "vip", "resource_clone_meta": []any{"clone-max=3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerResourceCleanup(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs resource status": {
			{RC: 0, Stdout: "vip: Started (failed)"},
			{RC: 0, Stdout: "vip: Started"},
		},
		"pcs resource cleanup": {{RC: 0}},
	})
	res, err := modulePacemakerResource(context.Background(), conn, map[string]any{"state": "cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerResourceValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePacemakerResource(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
	if _, err := modulePacemakerResource(context.Background(), conn, map[string]any{"state": "present"}); err == nil {
		t.Fatal("want error: name required for present")
	}
	if _, err := modulePacemakerResource(context.Background(), conn, map[string]any{"state": "absent"}); err == nil {
		t.Fatal("want error: name required for absent")
	}
	if _, err := modulePacemakerResource(context.Background(), conn, map[string]any{
		"state": "present", "name": "vip",
	}); err == nil {
		t.Fatal("want error: resource_type required for present")
	}
}

func TestPacemakerResourceAgent(t *testing.T) {
	if got := pacemakerResourceAgent(map[string]any{"resource_name": "IPaddr2"}); got != "IPaddr2" {
		t.Fatalf("agent = %q", got)
	}
	full := map[string]any{"resource_standard": "ocf", "resource_provider": "heartbeat", "resource_name": "IPaddr2"}
	if got := pacemakerResourceAgent(full); got != "ocf:heartbeat:IPaddr2" {
		t.Fatalf("agent = %q", got)
	}
}
