package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacemakerStonithPresentCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs stonith status virtual-stonith": {
			{RC: 1, Stdout: ""},
			{RC: 0, Stdout: "  * virtual-stonith\t(stonith:fence_virt):\t Started"},
		},
		"pcs stonith create virtual-stonith fence_virt pcmk_host_list=f1 op monitor interval=30s": {{RC: 0}},
	})
	res, err := modulePacemakerStonith(context.Background(), conn, map[string]any{
		"state":           "present",
		"name":            "virtual-stonith",
		"stonith_type":    "fence_virt",
		"stonith_options": []any{"pcmk_host_list=f1"},
		"stonith_operations": []any{
			map[string]any{"operation_action": "monitor", "operation_options": []any{"interval=30s"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["value"] != "* virtual-stonith\t(stonith:fence_virt):\t Started" {
		t.Fatalf("value = %v", res.Extra["value"])
	}
	if res.Extra["previous_value"] != nil {
		t.Fatalf("previous_value = %v, want nil", res.Extra["previous_value"])
	}
}

func TestModulePacemakerStonithPresentAlreadyExistsTolerated(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs stonith status vfence": {{RC: 0, Stdout: "vfence: Started"}},
		"pcs stonith create vfence fence_virt": {
			{RC: 1, Stderr: "Error: 'vfence' already exists"},
		},
	})
	res, err := modulePacemakerStonith(context.Background(), conn, map[string]any{
		"state": "present", "name": "vfence", "stonith_type": "fence_virt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want the tolerated 'already exists' error not to fail the task")
	}
}

func TestModulePacemakerStonithAbsent(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs stonith status vfence": {
			{RC: 0, Stdout: "vfence: Started"},
			{RC: 1, Stdout: ""},
		},
		"pcs stonith remove vfence": {{RC: 0}},
	})
	res, err := modulePacemakerStonith(context.Background(), conn, map[string]any{
		"state": "absent", "name": "vfence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerStonithEnabled(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs stonith status vfence": {
			{RC: 0, Stdout: "vfence: Stopped"},
			{RC: 0, Stdout: "vfence: Started"},
		},
		"pcs stonith enable vfence": {{RC: 0}},
	})
	res, err := modulePacemakerStonith(context.Background(), conn, map[string]any{
		"state": "enabled", "name": "vfence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerStonithDisabled(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"pcs stonith status vfence": {
			{RC: 0, Stdout: "vfence: Started"},
			{RC: 0, Stdout: "vfence: Stopped"},
		},
		"pcs stonith disable vfence": {{RC: 0}},
	})
	res, err := modulePacemakerStonith(context.Background(), conn, map[string]any{
		"state": "disabled", "name": "vfence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacemakerStonithMetasEachOwnPair(t *testing.T) {
	if got := pacemakerStonithCreateCmd("n", "t", nil, nil, []string{"a", "b"}, nil, false); got != "pcs stonith create n t meta a meta b" {
		t.Fatalf("cmd = %q", got)
	}
}

func TestModulePacemakerStonithArgumentGroup(t *testing.T) {
	got := stonithArgument(map[string]any{"argument_action": "group", "argument_options": []any{"g1"}})
	if len(got) != 2 || got[0] != "--group" || got[1] != "g1" {
		t.Fatalf("argument = %v", got)
	}
	got = stonithArgument(map[string]any{"argument_action": "before", "argument_options": []any{"other"}})
	if len(got) != 2 || got[0] != "before" || got[1] != "other" {
		t.Fatalf("argument = %v", got)
	}
}

func TestModulePacemakerStonithAgentValidation(t *testing.T) {
	got := pacemakerStonithCreateCmd("n", "t", nil, nil, nil, nil, true)
	if got != "pcs stonith create n t --agent-validation" {
		t.Fatalf("cmd = %q", got)
	}
}

func TestModulePacemakerStonithValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePacemakerStonith(context.Background(), conn, map[string]any{"state": "bogus", "name": "x"}); err == nil {
		t.Fatal("want error for bad state")
	}
	if _, err := modulePacemakerStonith(context.Background(), conn, map[string]any{"state": "present"}); err == nil {
		t.Fatal("want error: name required")
	}
	if _, err := modulePacemakerStonith(context.Background(), conn, map[string]any{
		"state": "present", "name": "x",
	}); err == nil {
		t.Fatal("want error: stonith_type required for present")
	}
}
