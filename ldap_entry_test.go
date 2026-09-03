package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLdapEntryCreate(t *testing.T) {
	existCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -b ou=users,dc=example,dc=com -s base '(objectClass=*)' 1.1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch":            {RC: 0},
		"command -v ldapadd":               {RC: 0},
		existCmd:                           {RC: 32}, // no such object -> absent
		"ldapadd -H ldapi:/// -Y EXTERNAL": {RC: 0},
	})
	res, err := moduleLdapEntry(context.Background(), fc, map[string]any{
		"dn":               "ou=users,dc=example,dc=com",
		"objectClass":      "organizationalUnit",
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(fc.Stdins) != 4 {
		t.Fatalf("Stdins = %v", fc.Stdins)
	}
	ldif := fc.Stdins[3]
	if !strings.Contains(ldif, "dn: ou=users,dc=example,dc=com\n") {
		t.Fatalf("ldif = %q, missing dn line", ldif)
	}
	if !strings.Contains(ldif, "changetype: add\n") {
		t.Fatalf("ldif = %q, missing changetype", ldif)
	}
	if !strings.Contains(ldif, "objectClass: organizationalUnit\n") {
		t.Fatalf("ldif = %q, missing objectClass", ldif)
	}
}

func TestModuleLdapEntryCreateWithAttributes(t *testing.T) {
	existCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -b cn=admin,dc=example,dc=com -s base '(objectClass=*)' 1.1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch":            {RC: 0},
		"command -v ldapadd":               {RC: 0},
		existCmd:                           {RC: 32},
		"ldapadd -H ldapi:/// -Y EXTERNAL": {RC: 0},
	})
	res, err := moduleLdapEntry(context.Background(), fc, map[string]any{
		"dn":          "cn=admin,dc=example,dc=com",
		"objectClass": []any{"simpleSecurityObject", "organizationalRole"},
		"attributes": map[string]any{
			"description": "An LDAP administrator",
		},
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	ldif := fc.Stdins[len(fc.Stdins)-1]
	if !strings.Contains(ldif, "objectClass: simpleSecurityObject\n") || !strings.Contains(ldif, "objectClass: organizationalRole\n") {
		t.Fatalf("ldif = %q", ldif)
	}
	if !strings.Contains(ldif, "description: An LDAP administrator\n") {
		t.Fatalf("ldif = %q", ldif)
	}
}

func TestModuleLdapEntryAlreadyPresent(t *testing.T) {
	existCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -b ou=users,dc=example,dc=com -s base '(objectClass=*)' 1.1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapadd":    {RC: 0},
		existCmd:                {RC: 0}, // present
	})
	res, err := moduleLdapEntry(context.Background(), fc, map[string]any{
		"dn": "ou=users,dc=example,dc=com", "objectClass": "organizationalUnit", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when entry already present")
	}
}

func TestModuleLdapEntryDeleteRecursive(t *testing.T) {
	existCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -b ou=stuff,dc=example,dc=com -s base '(objectClass=*)' 1.1"
	delCmd := "ldapdelete -H ldapi:/// -Y EXTERNAL -r ou=stuff,dc=example,dc=com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapdelete": {RC: 0},
		existCmd:                {RC: 0}, // present
		delCmd:                  {RC: 0},
	})
	res, err := moduleLdapEntry(context.Background(), fc, map[string]any{
		"dn": "ou=stuff,dc=example,dc=com", "state": "absent", "recursive": true, "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range fc.Commands {
		if c == delCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want %q", fc.Commands, delCmd)
	}
}

func TestModuleLdapEntryAlreadyAbsent(t *testing.T) {
	existCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -b ou=stuff,dc=example,dc=com -s base '(objectClass=*)' 1.1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapdelete": {RC: 0},
		existCmd:                {RC: 32}, // absent
	})
	res, err := moduleLdapEntry(context.Background(), fc, map[string]any{
		"dn": "ou=stuff,dc=example,dc=com", "state": "absent", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when already absent")
	}
}

func TestModuleLdapEntryMissingObjectClass(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleLdapEntry(context.Background(), fc, map[string]any{"dn": "ou=x,dc=example,dc=com"}); err == nil {
		t.Fatal("want error: objectClass required for state=present")
	}
}

func TestModuleLdapEntryMissingBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 1},
	})
	res, err := moduleLdapEntry(context.Background(), fc, map[string]any{
		"dn": "ou=x,dc=example,dc=com", "objectClass": "organizationalUnit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when ldapsearch is missing")
	}
}
