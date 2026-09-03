package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaOtpconfigNoop(t *testing.T) {
	showCmd := "ipa otpconfig-show --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  ipatokentotpauthwindow: 300\n"},
	})
	res, err := moduleIpaOtpconfig(context.Background(), fc, map[string]any{
		"ipatokentotpauthwindow": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaOtpconfigChangeViaAlias(t *testing.T) {
	showCmd := "ipa otpconfig-show --all --raw"
	modCmd := "ipa otpconfig-mod --ipatokenhotpsyncwindow=100"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: ""},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaOtpconfig(context.Background(), fc, map[string]any{
		"hotpsyncwindow": 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaOtpconfigNoBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 1},
	})
	res, err := moduleIpaOtpconfig(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
