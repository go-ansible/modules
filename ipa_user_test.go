package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaUserCreate(t *testing.T) {
	showCmd := "ipa user-show pinky --all --raw"
	addCmd := "ipa user-add pinky --givenname=Pinky --sn=Acme --gidnumber=100 --mail=pinky@acme.com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{
		"uid": "pinky", "givenname": "Pinky", "sn": "Acme", "gidnumber": "100", "mail": []any{"pinky@acme.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range fc.Commands {
		if c == addCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want %q", fc.Commands, addCmd)
	}
}

func TestModuleIpaUserCreateMissingRequired(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa":                  {RC: 0},
		"ipa user-show pinky --all --raw": {RC: 2},
	})
	if _, err := moduleIpaUser(context.Background(), fc, map[string]any{"uid": "pinky"}); err == nil {
		t.Fatal("want error: givenname/sn required to create a user")
	}
}

func TestModuleIpaUserAlreadyPresentNoChange(t *testing.T) {
	showCmd := "ipa user-show pinky --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  uid: pinky\n  givenname: Pinky\n  sn: Acme\n  gidnumber: 100\n"},
	})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{
		"uid": "pinky", "gidnumber": "100",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleIpaUserModify(t *testing.T) {
	showCmd := "ipa user-show pinky --all --raw"
	modCmd := "ipa user-mod pinky --gidnumber=200"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  uid: pinky\n  gidnumber: 100\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{
		"uid": "pinky", "gidnumber": "200",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range fc.Commands {
		if c == modCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want %q", fc.Commands, modCmd)
	}
}

func TestModuleIpaUserPasswordAlwaysSent(t *testing.T) {
	showCmd := "ipa user-show pinky --all --raw"
	modCmd := "ipa user-mod pinky --userpassword=zounds"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  uid: pinky\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{
		"uid": "pinky", "password": "zounds",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want changed (password always re-sent with update_password=always)", res)
	}
}

func TestModuleIpaUserPasswordOnCreateSkipsExisting(t *testing.T) {
	showCmd := "ipa user-show pinky --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  uid: pinky\n"},
	})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{
		"uid": "pinky", "password": "zounds", "update_password": "on_create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged (update_password=on_create must not touch an existing user)", res)
	}
}

func TestModuleIpaUserDelete(t *testing.T) {
	showCmd := "ipa user-show brain --all --raw"
	delCmd := "ipa user-del brain"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  uid: brain\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{"uid": "brain", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaUserDisable(t *testing.T) {
	showCmd := "ipa user-show pinky --all --raw"
	disableCmd := "ipa user-disable pinky"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  uid: pinky\n  nsaccountlock: FALSE\n"},
		disableCmd:       {RC: 0},
	})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{"uid": "pinky", "state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaUserAlreadyDisabled(t *testing.T) {
	showCmd := "ipa user-show pinky --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  uid: pinky\n  nsaccountlock: TRUE\n"},
	})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{"uid": "pinky", "state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged (already disabled)", res)
	}
}

func TestModuleIpaUserMissingUID(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaUser(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing uid")
	}
}

func TestModuleIpaUserMissingBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{"command -v ipa": {RC: 1}})
	res, err := moduleIpaUser(context.Background(), fc, map[string]any{"uid": "pinky"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when ipa binary is missing")
	}
}
