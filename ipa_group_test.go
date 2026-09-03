package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaGroupCreate(t *testing.T) {
	showCmd := "ipa group-show oinstall --all --raw"
	addCmd := "ipa group-add oinstall --gidnumber=54321"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaGroup(context.Background(), fc, map[string]any{"cn": "oinstall", "gidnumber": "54321"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGroupAppendAddsOnly(t *testing.T) {
	showCmd := "ipa group-show developers --all --raw"
	addMemberCmd := "ipa group-add-member developers --user=john"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd: {RC: 0, Stdout: "  cn: developers\n" +
			"  member: uid=alice,cn=users,cn=accounts,dc=example,dc=com\n"},
		addMemberCmd: {RC: 0},
	})
	res, err := moduleIpaGroup(context.Background(), fc, map[string]any{
		"cn": "developers", "user": []any{"john"}, "append": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	// append=true must never remove alice.
	for _, c := range fc.Commands {
		if c == "ipa group-remove-member developers --user=alice" {
			t.Fatal("append=true must not remove existing members")
		}
	}
}

func TestModuleIpaGroupExactReplacesMembers(t *testing.T) {
	showCmd := "ipa group-show sysops --all --raw"
	removeMemberCmd := "ipa group-remove-member sysops --user=alice"
	addMemberCmd := "ipa group-add-member sysops --user=larry --user=linus"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd: {RC: 0, Stdout: "  cn: sysops\n" +
			"  member: uid=alice,cn=users,cn=accounts,dc=example,dc=com\n"},
		removeMemberCmd: {RC: 0},
		addMemberCmd:    {RC: 0},
	})
	res, err := moduleIpaGroup(context.Background(), fc, map[string]any{
		"cn": "sysops", "user": []any{"linus", "larry"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	foundAdd, foundRemove := false, false
	for _, c := range fc.Commands {
		if c == addMemberCmd {
			foundAdd = true
		}
		if c == removeMemberCmd {
			foundRemove = true
		}
	}
	if !foundAdd || !foundRemove {
		t.Fatalf("commands = %v", fc.Commands)
	}
}

func TestModuleIpaGroupNestedGroupMembers(t *testing.T) {
	showCmd := "ipa group-show ops --all --raw"
	addMemberCmd := "ipa group-add-member ops --group=appops --group=sysops"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: ops\n"},
		addMemberCmd:     {RC: 0},
	})
	res, err := moduleIpaGroup(context.Background(), fc, map[string]any{
		"cn": "ops", "group": []any{"sysops", "appops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGroupExternalUserAlwaysApplied(t *testing.T) {
	showCmd := "ipa group-show developers --all --raw"
	extCmd := "ipa group-add-member developers --ipaexternalmember=S-1-5-21-123-1234-12345-63421"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: developers\n  external: TRUE\n"},
		extCmd:           {RC: 0},
	})
	res, err := moduleIpaGroup(context.Background(), fc, map[string]any{
		"cn": "developers", "external": true, "external_user": []any{"S-1-5-21-123-1234-12345-63421"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGroupDelete(t *testing.T) {
	showCmd := "ipa group-show sysops --all --raw"
	delCmd := "ipa group-del sysops"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: sysops\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaGroup(context.Background(), fc, map[string]any{"cn": "sysops", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGroupMissingCN(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaGroup(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing cn")
	}
}
