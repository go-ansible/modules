package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const ldapIncDN = "cn=uidNext,ou=unix-management,dc=example,dc=com"

func TestModuleLdapIncReadOnlyWhenIncrementZero(t *testing.T) {
	readCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b " + ldapIncDN + " -s base '(objectClass=*)' uidNumber"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapmodify": {RC: 0},
		readCmd:                 {RC: 0, Stdout: "dn: " + ldapIncDN + "\nuidNumber: 41\n\n"},
	})
	res, err := moduleLdapInc(context.Background(), fc, map[string]any{
		"dn": ldapIncDN, "attribute": "uidNumber", "increment": 0, "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["value"] != "41" || res.Extra["incremented"] != false {
		t.Fatalf("value/incremented = %v/%v", res.Extra["value"], res.Extra["incremented"])
	}
}

func TestModuleLdapIncRFC4525Auto(t *testing.T) {
	rootDSECmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b '' -s base '(objectClass=*)' supportedControl supportedFeatures supportedExtension"
	readCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b " + ldapIncDN + " -s base '(objectClass=*)' uidNumber"
	modifyCmd := "ldapmodify -H ldapi:/// -Y EXTERNAL"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapmodify": {RC: 0},
		rootDSECmd:              {RC: 0, Stdout: "dn:\nsupportedControl: 1.3.6.1.1.13.2\nsupportedFeatures: 1.3.6.1.1.14\n\n"},
		modifyCmd:               {RC: 0},
		readCmd:                 {RC: 0, Stdout: "dn: " + ldapIncDN + "\nuidNumber: 42\n\n"},
	})
	res, err := moduleLdapInc(context.Background(), fc, map[string]any{
		"dn": ldapIncDN, "attribute": "uidNumber", "increment": 1, "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["rfc4525"] != true {
		t.Fatalf("rfc4525 = %v, want true", res.Extra["rfc4525"])
	}
	if res.Extra["value"] != "42" {
		t.Fatalf("value = %v", res.Extra["value"])
	}
	ldif := fc.Stdins[len(fc.Stdins)-2] // the ldapmodify call precedes the follow-up read
	if !strings.Contains(ldif, "increment: uidNumber\n") {
		t.Fatalf("ldif = %q, want an increment: mod-spec", ldif)
	}
}

func TestModuleLdapIncLegacyForced(t *testing.T) {
	readCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b " + ldapIncDN + " -s base '(objectClass=*)' uidNumber"
	modifyCmd := "ldapmodify -H ldapi:/// -Y EXTERNAL"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapmodify": {RC: 0},
		readCmd:                 {RC: 0, Stdout: "dn: " + ldapIncDN + "\nuidNumber: 41\n\n"},
		modifyCmd:               {RC: 0},
	})
	res, err := moduleLdapInc(context.Background(), fc, map[string]any{
		"dn": ldapIncDN, "attribute": "uidNumber", "increment": 1, "method": "legacy", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["rfc4525"] != false {
		t.Fatalf("rfc4525 = %v, want false", res.Extra["rfc4525"])
	}
	if res.Extra["value"] != "42" {
		t.Fatalf("value = %v, want 42", res.Extra["value"])
	}
	ldif := fc.Stdins[len(fc.Stdins)-1]
	if !strings.Contains(ldif, "delete: uidNumber\n") || !strings.Contains(ldif, "add: uidNumber\n") {
		t.Fatalf("ldif = %q", ldif)
	}
}

func TestModuleLdapIncEntryNotFound(t *testing.T) {
	readCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b " + ldapIncDN + " -s base '(objectClass=*)' uidNumber"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapmodify": {RC: 0},
		readCmd:                 {RC: 32}, // no such object
	})
	res, err := moduleLdapInc(context.Background(), fc, map[string]any{
		"dn": ldapIncDN, "attribute": "uidNumber", "increment": 1, "method": "legacy", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the entry/attribute does not exist")
	}
}

func TestModuleLdapIncMissingArgs(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleLdapInc(context.Background(), fc, map[string]any{"attribute": "uidNumber"}); err == nil {
		t.Fatal("want error for missing dn")
	}
	if _, err := moduleLdapInc(context.Background(), fc, map[string]any{"dn": ldapIncDN}); err == nil {
		t.Fatal("want error for missing attribute")
	}
}

func TestModuleLdapIncMissingBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 1},
	})
	res, err := moduleLdapInc(context.Background(), fc, map[string]any{"dn": ldapIncDN, "attribute": "uidNumber"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when ldapsearch is missing")
	}
}
