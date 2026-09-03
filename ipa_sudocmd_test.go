package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaSudocmdCreate(t *testing.T) {
	showCmd := "ipa sudocmd-show su --all --raw"
	addCmd := "ipa sudocmd-add su '--description=Allow to run su via sudo'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaSudocmd(context.Background(), fc, map[string]any{
		"name": "su", "description": "Allow to run su via sudo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSudocmdAbsent(t *testing.T) {
	showCmd := "ipa sudocmd-show su --all --raw"
	delCmd := "ipa sudocmd-del su"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  sudocmd: su\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaSudocmd(context.Background(), fc, map[string]any{
		"sudocmd": "su", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

// This is the documented real-ipa_sudocmd quirk: state=enabled/disabled
// deletes the object, exactly like state=absent, because real
// ipa_sudocmd never checks for those two values.
func TestModuleIpaSudocmdEnabledStateActuallyDeletes(t *testing.T) {
	showCmd := "ipa sudocmd-show su --all --raw"
	delCmd := "ipa sudocmd-del su"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  sudocmd: su\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaSudocmd(context.Background(), fc, map[string]any{
		"sudocmd": "su", "state": "enabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed (deleted) for state=enabled, matching real ipa_sudocmd's own quirk", res)
	}
	found := false
	for _, c := range fc.Commands {
		if c == delCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want sudocmd-del even though state=enabled", fc.Commands)
	}
}

func TestModuleIpaSudocmdNoop(t *testing.T) {
	showCmd := "ipa sudocmd-show su --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  sudocmd: su\n  description: already set\n"},
	})
	res, err := moduleIpaSudocmd(context.Background(), fc, map[string]any{
		"sudocmd": "su", "description": "already set",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
