package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaGetkeytabCreate(t *testing.T) {
	testCmd := "test -e /etc/ipa/test.keytab"
	getCmd := "ipa-getkeytab --keytab=/etc/ipa/test.keytab --principal=HTTP/freeipa-dc02.ipa.test --server=freeipa-dc01.ipa.test"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa-getkeytab": {RC: 0},
		testCmd:                    {RC: 1},
		getCmd:                     {RC: 0},
	})
	res, err := moduleIpaGetkeytab(context.Background(), fc, map[string]any{
		"path":      "/etc/ipa/test.keytab",
		"principal": "HTTP/freeipa-dc02.ipa.test",
		"ipa_host":  "freeipa-dc01.ipa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGetkeytabAlreadyExistsNoForce(t *testing.T) {
	testCmd := "test -e /etc/ipa/test.keytab"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa-getkeytab": {RC: 0},
		testCmd:                    {RC: 0},
	})
	res, err := moduleIpaGetkeytab(context.Background(), fc, map[string]any{
		"path":      "/etc/ipa/test.keytab",
		"principal": "HTTP/freeipa-dc02.ipa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(fc.Commands) != 2 {
		t.Fatalf("commands = %v, want exactly the PATH check and the existence test, no ipa-getkeytab call", fc.Commands)
	}
}

func TestModuleIpaGetkeytabForceRecreates(t *testing.T) {
	testCmd := "test -e /etc/ipa/test.keytab"
	getCmd := "ipa-getkeytab --keytab=/etc/ipa/test.keytab --principal=HTTP/x"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa-getkeytab": {RC: 0},
		testCmd:                    {RC: 0},
		getCmd:                     {RC: 0},
	})
	res, err := moduleIpaGetkeytab(context.Background(), fc, map[string]any{
		"path": "/etc/ipa/test.keytab", "principal": "HTTP/x", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGetkeytabAbsentRemoves(t *testing.T) {
	testCmd := "test -e /etc/ipa/test.keytab"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa-getkeytab": {RC: 0},
		testCmd:                    {RC: 0},
	})
	res, err := moduleIpaGetkeytab(context.Background(), fc, map[string]any{
		"path": "/etc/ipa/test.keytab", "principal": "HTTP/x", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGetkeytabRetrieveMode(t *testing.T) {
	testCmd := "test -e /etc/ipa/test.keytab"
	getCmd := "ipa-getkeytab --keytab=/etc/ipa/test.keytab --principal=HTTP/x --retrieve"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa-getkeytab": {RC: 0},
		testCmd:                    {RC: 1},
		getCmd:                     {RC: 0},
	})
	res, err := moduleIpaGetkeytab(context.Background(), fc, map[string]any{
		"path": "/etc/ipa/test.keytab", "principal": "HTTP/x", "retrieve_mode": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGetkeytabNoBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa-getkeytab": {RC: 1},
	})
	res, err := moduleIpaGetkeytab(context.Background(), fc, map[string]any{
		"path": "/x", "principal": "HTTP/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaGetkeytabMutuallyExclusive(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaGetkeytab(context.Background(), fc, map[string]any{
		"path": "/x", "principal": "HTTP/x", "ipa_host": "a", "ldap_uri": "ldap://b",
	}); err == nil {
		t.Fatal("want error: ipa_host and ldap_uri are mutually exclusive")
	}
}
