package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLdapAttrsPresentAdds(t *testing.T) {
	getCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b cn=admin,dc=example,dc=com -s base '(objectClass=*)' description"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch":               {RC: 0},
		"command -v ldapmodify":               {RC: 0},
		getCmd:                                {RC: 0}, // no matching entry -> no current values
		"ldapmodify -H ldapi:/// -Y EXTERNAL": {RC: 0},
	})
	res, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn":               "cn=admin,dc=example,dc=com",
		"attributes":       map[string]any{"description": "An LDAP administrator"},
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	ldif := fc.Stdins[len(fc.Stdins)-1]
	if !strings.Contains(ldif, "add: description\n") || !strings.Contains(ldif, "description: An LDAP administrator\n") {
		t.Fatalf("ldif = %q", ldif)
	}
}

func TestModuleLdapAttrsPresentAlreadySet(t *testing.T) {
	getCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b cn=admin,dc=example,dc=com -s base '(objectClass=*)' description"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapmodify": {RC: 0},
		getCmd:                  {RC: 0, Stdout: "dn: cn=admin,dc=example,dc=com\ndescription: An LDAP administrator\n\n"},
	})
	res, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn":               "cn=admin,dc=example,dc=com",
		"attributes":       map[string]any{"description": "An LDAP administrator"},
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleLdapAttrsAbsentRemoves(t *testing.T) {
	getCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b uid=jdoe,ou=people,dc=example,dc=com -s base '(objectClass=*)' description"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch":               {RC: 0},
		"command -v ldapmodify":               {RC: 0},
		getCmd:                                {RC: 0, Stdout: "dn: uid=jdoe,ou=people,dc=example,dc=com\ndescription: An example user account\n\n"},
		"ldapmodify -H ldapi:/// -Y EXTERNAL": {RC: 0},
	})
	res, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn":               "uid=jdoe,ou=people,dc=example,dc=com",
		"attributes":       map[string]any{"description": "An example user account"},
		"state":            "absent",
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	ldif := fc.Stdins[len(fc.Stdins)-1]
	if !strings.Contains(ldif, "delete: description\n") {
		t.Fatalf("ldif = %q", ldif)
	}
}

func TestModuleLdapAttrsExactReplace(t *testing.T) {
	getCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b 'olcDatabase={1}hdb,cn=config' -s base '(objectClass=*)' olcSuffix"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch":               {RC: 0},
		"command -v ldapmodify":               {RC: 0},
		getCmd:                                {RC: 0, Stdout: "dn: olcDatabase={1}hdb,cn=config\nolcSuffix: dc=old,dc=com\n\n"},
		"ldapmodify -H ldapi:/// -Y EXTERNAL": {RC: 0},
	})
	res, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn":               "olcDatabase={1}hdb,cn=config",
		"attributes":       map[string]any{"olcSuffix": "dc=example,dc=com"},
		"state":            "exact",
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	ldif := fc.Stdins[len(fc.Stdins)-1]
	if !strings.Contains(ldif, "replace: olcSuffix\n") || !strings.Contains(ldif, "olcSuffix: dc=example,dc=com\n") {
		t.Fatalf("ldif = %q", ldif)
	}
}

func TestModuleLdapAttrsExactAddWhenAbsent(t *testing.T) {
	getCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b 'olcDatabase={1}hdb,cn=config' -s base '(objectClass=*)' olcSuffix"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch":               {RC: 0},
		"command -v ldapmodify":               {RC: 0},
		getCmd:                                {RC: 0}, // absent
		"ldapmodify -H ldapi:/// -Y EXTERNAL": {RC: 0},
	})
	res, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn":               "olcDatabase={1}hdb,cn=config",
		"attributes":       map[string]any{"olcSuffix": "dc=example,dc=com"},
		"state":            "exact",
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	ldif := fc.Stdins[len(fc.Stdins)-1]
	if !strings.Contains(ldif, "add: olcSuffix\n") {
		t.Fatalf("ldif = %q, want add: for a previously-absent attribute", ldif)
	}
	_ = res
}

func TestModuleLdapAttrsExactEmptyDeletesAll(t *testing.T) {
	getCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b uid=jdoe,ou=people,dc=example,dc=com -s base '(objectClass=*)' description"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch":               {RC: 0},
		"command -v ldapmodify":               {RC: 0},
		getCmd:                                {RC: 0, Stdout: "dn: uid=jdoe,ou=people,dc=example,dc=com\ndescription: An example user account\n\n"},
		"ldapmodify -H ldapi:/// -Y EXTERNAL": {RC: 0},
	})
	res, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn":               "uid=jdoe,ou=people,dc=example,dc=com",
		"attributes":       map[string]any{"description": []any{}},
		"state":            "exact",
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	ldif := fc.Stdins[len(fc.Stdins)-1]
	if !strings.Contains(ldif, "delete: description\n") {
		t.Fatalf("ldif = %q", ldif)
	}
}

func TestModuleLdapAttrsOrdered(t *testing.T) {
	getCmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b 'olcDatabase={1}hdb,cn=config' -s base '(objectClass=*)' olcAccess"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch":               {RC: 0},
		"command -v ldapmodify":               {RC: 0},
		getCmd:                                {RC: 0},
		"ldapmodify -H ldapi:/// -Y EXTERNAL": {RC: 0},
	})
	res, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn":               "olcDatabase={1}hdb,cn=config",
		"attributes":       map[string]any{"olcAccess": []any{"to attrs=userPassword", "to dn.base=\"dc=example,dc=com\""}},
		"ordered":          true,
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	ldif := fc.Stdins[len(fc.Stdins)-1]
	if !strings.Contains(ldif, "olcAccess: {0}to attrs=userPassword\n") {
		t.Fatalf("ldif = %q, want X-ORDERed {0} prefix", ldif)
	}
	if !strings.Contains(ldif, `{1}to dn.base="dc=example,dc=com"`) {
		t.Fatalf("ldif = %q, want X-ORDERed {1} prefix", ldif)
	}
}

func TestModuleLdapAttrsBadBase64(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		"command -v ldapmodify": {RC: 0},
	})
	res, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn":                "cn=x,dc=example,dc=com",
		"attributes":        map[string]any{"jpegPhoto": "not-valid-base64!!"},
		"binary_attributes": []any{"jpegPhoto"},
		"xorder_discovery":  "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for invalid base64 in a binary attribute")
	}
}

func TestModuleLdapAttrsMissingAttributes(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleLdapAttrs(context.Background(), fc, map[string]any{"dn": "cn=x,dc=example,dc=com"}); err == nil {
		t.Fatal("want error for missing attributes")
	}
}

func TestModuleLdapAttrsInvalidState(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleLdapAttrs(context.Background(), fc, map[string]any{
		"dn": "cn=x,dc=example,dc=com", "attributes": map[string]any{"cn": "x"}, "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
