package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaSudocmdgroupCreateWithMembers(t *testing.T) {
	showCmd := "ipa sudocmdgroup-show group01 --all --raw"
	addCmd := "ipa sudocmdgroup-add group01 '--description=Group of important commands'"
	addMemberCmd := "ipa sudocmdgroup-add-member group01 --sudocmd=su"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
		addMemberCmd:     {RC: 0},
	})

	res, err := moduleIpaSudocmdgroup(context.Background(), fc, map[string]any{
		"name": "group01", "description": "Group of important commands", "sudocmd": []any{"su"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSudocmdgroupAbsent(t *testing.T) {
	showCmd := "ipa sudocmdgroup-show group01 --all --raw"
	delCmd := "ipa sudocmdgroup-del group01"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: group01\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaSudocmdgroup(context.Background(), fc, map[string]any{
		"cn": "group01", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

// Same real-module quirk as ipa_sudocmd: state=disabled deletes.
func TestModuleIpaSudocmdgroupDisabledStateActuallyDeletes(t *testing.T) {
	showCmd := "ipa sudocmdgroup-show group01 --all --raw"
	delCmd := "ipa sudocmdgroup-del group01"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: group01\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaSudocmdgroup(context.Background(), fc, map[string]any{
		"cn": "group01", "state": "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed (deleted)", res)
	}
}

func TestModuleIpaSudocmdgroupMemberReconcile(t *testing.T) {
	showCmd := "ipa sudocmdgroup-show group01 --all --raw"
	addMemberCmd := "ipa sudocmdgroup-add-member group01 --sudocmd=ls"
	removeMemberCmd := "ipa sudocmdgroup-remove-member group01 --sudocmd=su"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: group01\n  member_sudocmd: su\n"},
		addMemberCmd:     {RC: 0},
		removeMemberCmd:  {RC: 0},
	})
	res, err := moduleIpaSudocmdgroup(context.Background(), fc, map[string]any{
		"cn": "group01", "sudocmd": []any{"ls"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}
