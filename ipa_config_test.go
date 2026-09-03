package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaConfigNoop(t *testing.T) {
	showCmd := "ipa config-show --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  ipadefaultloginshell: /bin/bash\n"},
	})
	res, err := moduleIpaConfig(context.Background(), fc, map[string]any{
		"ipadefaultloginshell": "/bin/bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaConfigScalarChange(t *testing.T) {
	showCmd := "ipa config-show --all --raw"
	modCmd := "ipa config-mod --ipadefaultloginshell=/bin/zsh"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  ipadefaultloginshell: /bin/bash\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaConfig(context.Background(), fc, map[string]any{
		"loginshell": "/bin/zsh", // via alias
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaConfigListAndJoined(t *testing.T) {
	showCmd := "ipa config-show --all --raw"
	modCmd := "ipa config-mod --ipauserauthtype=password --ipausersearchfields=uid,givenname,sn"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: ""},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaConfig(context.Background(), fc, map[string]any{
		"ipauserauthtype":     []any{"password"},
		"ipausersearchfields": []any{"uid", "givenname", "sn"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaConfigNoBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 1},
	})
	res, err := moduleIpaConfig(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
