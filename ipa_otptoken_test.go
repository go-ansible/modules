package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaOtptokenCreateTotp(t *testing.T) {
	showCmd := "ipa otptoken-show Token123 --all --raw"
	addCmd := "ipa otptoken-add Token123 --type=TOTP --ipatokenowner=pinky --ipatokendisabled=FALSE"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaOtptoken(context.Background(), fc, map[string]any{
		"uniqueid": "Token123", "otptype": "totp", "owner": "pinky",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaOtptokenCreateRenamedBeforeCreation(t *testing.T) {
	showOld := "ipa otptoken-show Token123 --all --raw"
	showNew := "ipa otptoken-show TokenABC --all --raw"
	addCmd := "ipa otptoken-add TokenABC --ipatokendisabled=FALSE"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showOld:          {RC: 2},
		showNew:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaOtptoken(context.Background(), fc, map[string]any{
		"uniqueid": "Token123", "newuniqueid": "TokenABC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	// It must have created under the NEW name, not the old one.
	found := false
	for _, c := range fc.Commands {
		if c == addCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want otptoken-add under TokenABC", fc.Commands)
	}
}

func TestModuleIpaOtptokenNewuniqueidAlreadyTaken(t *testing.T) {
	showOld := "ipa otptoken-show Token123 --all --raw"
	showNew := "ipa otptoken-show TokenABC --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showOld:          {RC: 2},
		showNew:          {RC: 0, Stdout: "  ipatokenuniqueid: TokenABC\n"},
	})
	res, err := moduleIpaOtptoken(context.Background(), fc, map[string]any{
		"uniqueid": "Token123", "newuniqueid": "TokenABC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed: new unique id already in use", res)
	}
}

func TestModuleIpaOtptokenRename(t *testing.T) {
	showOld := "ipa otptoken-show Token123 --all --raw"
	showNew := "ipa otptoken-show TokenABC --all --raw"
	modCmd := "ipa otptoken-mod Token123 --ipatokendisabled=FALSE --rename=TokenABC"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showOld:          {RC: 0, Stdout: "  ipatokenuniqueid: Token123\n  ipatokendisabled: TRUE\n"},
		showNew:          {RC: 2},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaOtptoken(context.Background(), fc, map[string]any{
		"uniqueid": "Token123", "newuniqueid": "TokenABC", "enabled": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaOtptokenUnmodifiableConflict(t *testing.T) {
	showCmd := "ipa otptoken-show Token123 --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  ipatokenuniqueid: Token123\n  type: TOTP\n"},
	})
	res, err := moduleIpaOtptoken(context.Background(), fc, map[string]any{
		"uniqueid": "Token123", "otptype": "hotp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed: otptype cannot be changed once created", res)
	}
}

func TestModuleIpaOtptokenModDescription(t *testing.T) {
	showCmd := "ipa otptoken-show Token123 --all --raw"
	modCmd := "ipa otptoken-mod Token123 --description=Acme"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  ipatokenuniqueid: Token123\n  ipatokendisabled: FALSE\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaOtptoken(context.Background(), fc, map[string]any{
		"uniqueid": "Token123", "description": "Acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaOtptokenAbsent(t *testing.T) {
	showCmd := "ipa otptoken-show Token123 --all --raw"
	delCmd := "ipa otptoken-del Token123"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  ipatokenuniqueid: Token123\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaOtptoken(context.Background(), fc, map[string]any{
		"uniqueid": "Token123", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}
