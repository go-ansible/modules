package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaRoleCreate(t *testing.T) {
	showCmd := "ipa role-show dba --all --raw"
	addCmd := "ipa role-add dba --description=Database Administrators"
	addMemberCmd := "ipa role-add-member dba --user=brain --user=pinky"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
		addMemberCmd:     {RC: 0},
	})
	res, err := moduleIpaRole(context.Background(), fc, map[string]any{
		"cn": "dba", "description": "Database Administrators", "user": []any{"pinky", "brain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaRoleReconcilesUserMembersExactly(t *testing.T) {
	showCmd := "ipa role-show dba --all --raw"
	removeCmd := "ipa role-remove-member dba --user=alice"
	addCmd := "ipa role-add-member dba --user=brain"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd: {RC: 0, Stdout: "  cn: dba\n" +
			"  member: uid=alice,cn=users,cn=accounts,dc=example,dc=com\n" +
			"  member: uid=pinky,cn=users,cn=accounts,dc=example,dc=com\n"},
		removeCmd: {RC: 0},
		addCmd:    {RC: 0},
	})
	res, err := moduleIpaRole(context.Background(), fc, map[string]any{
		"cn": "dba", "user": []any{"pinky", "brain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaRolePrivilegeAddOnly(t *testing.T) {
	showCmd := "ipa role-show dba --all --raw"
	addPrivCmd := "ipa role-add-privilege dba --privilege=Group Administrators"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: dba\n"},
		addPrivCmd:       {RC: 0},
	})
	res, err := moduleIpaRole(context.Background(), fc, map[string]any{
		"cn": "dba", "privilege": []any{"Group Administrators"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaRoleDelete(t *testing.T) {
	showCmd := "ipa role-show dba --all --raw"
	delCmd := "ipa role-del dba"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: dba\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaRole(context.Background(), fc, map[string]any{"cn": "dba", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaRoleMissingCN(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaRole(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing cn")
	}
}
