package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestLdapSearchScopeFlag(t *testing.T) {
	cases := map[string]string{
		"base":        "base",
		"onelevel":    "one",
		"subordinate": "children",
		"children":    "sub",
	}
	for in, want := range cases {
		got, err := ldapSearchScopeFlag(in)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("ldapSearchScopeFlag(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ldapSearchScopeFlag("bogus"); err == nil {
		t.Fatal("want error for invalid scope")
	}
}

func TestModuleLdapSearchBasic(t *testing.T) {
	cmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b ou=groups,dc=example,dc=com -s base '(objectClass=*)'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		cmd: {RC: 0, Stdout: "dn: uid=jdoe,ou=groups,dc=example,dc=com\n" +
			"cn: John Doe\n" +
			"gidNumber: 100\n" +
			"\n"},
	})
	res, err := moduleLdapSearch(context.Background(), fc, map[string]any{
		"dn": "ou=groups,dc=example,dc=com", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	results, ok := res.Extra["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v", res.Extra["results"])
	}
	entry := results[0].(map[string]any)
	if entry["dn"] != "uid=jdoe,ou=groups,dc=example,dc=com" {
		t.Fatalf("dn = %v", entry["dn"])
	}
	if entry["cn"] != "John Doe" {
		t.Fatalf("cn = %v", entry["cn"])
	}
	if entry["gidNumber"] != "100" {
		t.Fatalf("gidNumber = %v", entry["gidNumber"])
	}
}

func TestModuleLdapSearchMultiValue(t *testing.T) {
	cmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b ou=groups,dc=example,dc=com -s one '(objectClass=*)' gidNumber"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		cmd: {RC: 0, Stdout: "dn: cn=g1,ou=groups,dc=example,dc=com\n" +
			"gidNumber: 100\n" +
			"gidNumber: 200\n" +
			"\n"},
	})
	res, err := moduleLdapSearch(context.Background(), fc, map[string]any{
		"dn": "ou=groups,dc=example,dc=com", "scope": "onelevel", "attrs": []any{"gidNumber"},
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	results := res.Extra["results"].([]any)
	entry := results[0].(map[string]any)
	vals, ok := entry["gidNumber"].([]string)
	if !ok || len(vals) != 2 {
		t.Fatalf("gidNumber = %#v", entry["gidNumber"])
	}
}

func TestModuleLdapSearchSchema(t *testing.T) {
	cmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -A -b cn=admin,dc=example,dc=com -s base '(objectClass=*)'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		cmd: {RC: 0, Stdout: "dn: cn=admin,dc=example,dc=com\n" +
			"objectClass:\n" +
			"description:\n" +
			"\n"},
	})
	res, err := moduleLdapSearch(context.Background(), fc, map[string]any{
		"dn": "cn=admin,dc=example,dc=com", "schema": true, "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	results := res.Extra["results"].([]any)
	entry := results[0].(map[string]any)
	attrs, ok := entry["attrs"].([]string)
	if !ok || len(attrs) != 2 || attrs[0] != "description" || attrs[1] != "objectClass" {
		t.Fatalf("attrs = %#v", entry["attrs"])
	}
}

func TestModuleLdapSearchPageSize(t *testing.T) {
	cmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -E pr=5/noprompt -b dc=example,dc=com -s base '(objectClass=*)'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		cmd:                     {RC: 0},
	})
	_, err := moduleLdapSearch(context.Background(), fc, map[string]any{
		"dn": "dc=example,dc=com", "page_size": 5, "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range fc.Commands {
		if c == cmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want %q", fc.Commands, cmd)
	}
}

func TestModuleLdapSearchBase64Attributes(t *testing.T) {
	// raw bytes 0xff 0xfe are invalid UTF-8; base64 is "//4="
	cmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b cn=x,dc=example,dc=com -s base '(objectClass=*)'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		cmd: {RC: 0, Stdout: "dn: cn=x,dc=example,dc=com\n" +
			"jpegPhoto:: //4=\n" +
			"\n"},
	})
	res, err := moduleLdapSearch(context.Background(), fc, map[string]any{
		"dn": "cn=x,dc=example,dc=com", "base64_attributes": []any{"jpegPhoto"}, "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := res.Extra["results"].([]any)[0].(map[string]any)
	if entry["jpegPhoto"] != "//4=" {
		t.Fatalf("jpegPhoto = %v, want base64 preserved", entry["jpegPhoto"])
	}
}

func TestModuleLdapSearchInvalidUTF8NotBase64(t *testing.T) {
	cmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b cn=x,dc=example,dc=com -s base '(objectClass=*)'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		cmd: {RC: 0, Stdout: "dn: cn=x,dc=example,dc=com\n" +
			"jpegPhoto:: //4=\n" +
			"\n"},
	})
	res, err := moduleLdapSearch(context.Background(), fc, map[string]any{
		"dn": "cn=x,dc=example,dc=com", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := res.Extra["results"].([]any)[0].(map[string]any)
	got, ok := entry["jpegPhoto"].(string)
	if !ok || got == "//4=" {
		t.Fatalf("jpegPhoto = %v, want utf8-replaced (not left as base64)", entry["jpegPhoto"])
	}
}

func TestModuleLdapSearchFailure(t *testing.T) {
	cmd := "ldapsearch -H ldapi:/// -Y EXTERNAL -LLL -o ldif-wrap=no -b dc=nosuch,dc=com -s base '(objectClass=*)'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 0},
		cmd:                     {RC: 32, Stderr: "No such object"},
	})
	res, err := moduleLdapSearch(context.Background(), fc, map[string]any{
		"dn": "dc=nosuch,dc=com", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a nonzero ldapsearch exit")
	}
}

func TestModuleLdapSearchMissingBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapsearch": {RC: 1},
	})
	res, err := moduleLdapSearch(context.Background(), fc, map[string]any{"dn": "dc=example,dc=com"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when ldapsearch binary is missing")
	}
}

func TestModuleLdapSearchMissingDN(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleLdapSearch(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing dn")
	}
}

func TestModuleLdapSearchInvalidScope(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleLdapSearch(context.Background(), fc, map[string]any{"dn": "dc=example,dc=com", "scope": "bogus"}); err == nil {
		t.Fatal("want error for invalid scope")
	}
}
