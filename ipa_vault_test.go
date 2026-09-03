package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaVaultCreate(t *testing.T) {
	showCmd := "ipa vault-show vault01 --all --raw"
	addCmd := "ipa vault-add vault01 --ipavaulttype=standard --service=HTTP/foo"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaVault(context.Background(), fc, map[string]any{
		"name": "vault01", "ipavaulttype": "standard", "service": "HTTP/foo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaVaultUsernameMutuallyExclusiveWithService(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaVault(context.Background(), fc, map[string]any{
		"name": "vault01", "username": []any{"user01"}, "service": "HTTP/foo",
	}); err == nil {
		t.Fatal("want error: username and service are mutually exclusive")
	}
}

// This is the documented real-ipa_vault quirk: username/user is
// accepted but never sent anywhere, even though the real `ipa` CLI has
// a working --username flag for vault-add.
func TestModuleIpaVaultUsernameHasNoEffect(t *testing.T) {
	showCmd := "ipa vault-show vault01 --all --raw"
	addCmd := "ipa vault-add vault01 --ipavaulttype=standard"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaVault(context.Background(), fc, map[string]any{
		"name": "vault01", "ipavaulttype": "standard", "user": []any{"user01"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	for _, c := range fc.Commands {
		if c != showCmd && c != "command -v ipa" && c != addCmd {
			t.Fatalf("unexpected command %q — username must never be rendered into any ipa CLI flag", c)
		}
	}
}

// This is the documented real-ipa_vault quirk: without replace=true, an
// existing vault is left entirely untouched, even when other args differ.
func TestModuleIpaVaultExistingNoReplaceLeftUntouched(t *testing.T) {
	showCmd := "ipa vault-show vault01 --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: vault01\n  description: old\n"},
	})
	res, err := moduleIpaVault(context.Background(), fc, map[string]any{
		"name": "vault01", "description": "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, want unchanged: replace defaults to false", res)
	}
}

func TestModuleIpaVaultReplaceModifies(t *testing.T) {
	showCmd := "ipa vault-show vault01 --all --raw"
	modCmd := "ipa vault-mod vault01 --description=new"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: vault01\n  description: old\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaVault(context.Background(), fc, map[string]any{
		"name": "vault01", "description": "new", "replace": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaVaultAbsent(t *testing.T) {
	showCmd := "ipa vault-show vault01 --all --raw"
	delCmd := "ipa vault-del vault01"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: vault01\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaVault(context.Background(), fc, map[string]any{
		"name": "vault01", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}
