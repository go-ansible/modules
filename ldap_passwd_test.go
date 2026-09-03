package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLdapPasswdAlreadySet(t *testing.T) {
	checkCmd := "ldapwhoami -H ldapi:/// -x -D cn=admin,dc=example,dc=com -y /tmp/ldap-passwd"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapwhoami":               {RC: 0},
		"command -v ldappasswd":               {RC: 0},
		"umask 077 && cat > /tmp/ldap-passwd": {RC: 0},
		checkCmd:                              {RC: 0}, // bind succeeded -> already correct
	})
	res, err := moduleLdapPasswd(context.Background(), fc, map[string]any{
		"dn": "cn=admin,dc=example,dc=com", "passwd": "secret123", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	for _, c := range fc.Commands {
		if c == "ldappasswd -H ldapi:/// -Y EXTERNAL -T /tmp/ldap-passwd cn=admin,dc=example,dc=com" {
			t.Fatal("must not call ldappasswd when the password already matches")
		}
	}
}

func TestModuleLdapPasswdNeedsChange(t *testing.T) {
	checkCmd := "ldapwhoami -H ldapi:/// -x -D cn=admin,dc=example,dc=com -y /tmp/ldap-passwd"
	setCmd := "ldappasswd -H ldapi:/// -Y EXTERNAL -T /tmp/ldap-passwd cn=admin,dc=example,dc=com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapwhoami":               {RC: 0},
		"command -v ldappasswd":               {RC: 0},
		"umask 077 && cat > /tmp/ldap-passwd": {RC: 0},
		checkCmd:                              {RC: 49, Stderr: "Invalid credentials"},
		setCmd:                                {RC: 0},
	})
	res, err := moduleLdapPasswd(context.Background(), fc, map[string]any{
		"dn": "cn=admin,dc=example,dc=com", "passwd": "secret123", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range fc.Commands {
		if c == setCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want %q", fc.Commands, setCmd)
	}
}

func TestModuleLdapPasswdWithAdminBind(t *testing.T) {
	setCmd := "ldappasswd -H ldapi:/// -x -D cn=root,dc=example,dc=com -y /tmp/ldap-bindpw -T /tmp/ldap-passwd cn=admin,dc=example,dc=com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapwhoami":               {RC: 0},
		"command -v ldappasswd":               {RC: 0},
		"umask 077 && cat > /tmp/ldap-bindpw": {RC: 0},
		"umask 077 && cat > /tmp/ldap-passwd": {RC: 0},
		"ldapwhoami -H ldapi:/// -x -D cn=admin,dc=example,dc=com -y /tmp/ldap-passwd": {RC: 49},
		setCmd: {RC: 0},
	})
	res, err := moduleLdapPasswd(context.Background(), fc, map[string]any{
		"dn": "cn=admin,dc=example,dc=com", "passwd": "secret123",
		"bind_dn": "cn=root,dc=example,dc=com", "bind_pw": "rootpw",
		"xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range fc.Commands {
		if c == setCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want %q", fc.Commands, setCmd)
	}
}

func TestModuleLdapPasswdSetFails(t *testing.T) {
	checkCmd := "ldapwhoami -H ldapi:/// -x -D cn=admin,dc=example,dc=com -y /tmp/ldap-passwd"
	setCmd := "ldappasswd -H ldapi:/// -Y EXTERNAL -T /tmp/ldap-passwd cn=admin,dc=example,dc=com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapwhoami":               {RC: 0},
		"command -v ldappasswd":               {RC: 0},
		"umask 077 && cat > /tmp/ldap-passwd": {RC: 0},
		checkCmd:                              {RC: 49},
		setCmd:                                {RC: 1, Stderr: "insufficient access"},
	})
	res, err := moduleLdapPasswd(context.Background(), fc, map[string]any{
		"dn": "cn=admin,dc=example,dc=com", "passwd": "secret123", "xorder_discovery": "disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when ldappasswd itself fails")
	}
}

func TestModuleLdapPasswdMissingArgs(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleLdapPasswd(context.Background(), fc, map[string]any{"dn": "cn=admin,dc=example,dc=com"}); err == nil {
		t.Fatal("want error for missing passwd")
	}
	if _, err := moduleLdapPasswd(context.Background(), fc, map[string]any{"passwd": "x"}); err == nil {
		t.Fatal("want error for missing dn")
	}
}

func TestModuleLdapPasswdMissingBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ldapwhoami": {RC: 1},
	})
	res, err := moduleLdapPasswd(context.Background(), fc, map[string]any{
		"dn": "cn=admin,dc=example,dc=com", "passwd": "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when ldapwhoami is missing")
	}
}
