package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUdmUserMissingUsername(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUdmUser(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing username")
	}
}

func TestModuleUdmUserRequiresFirstLastPassword(t *testing.T) {
	findCmd := "udm users/user list --filter username=foo"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:             {RC: 0, Stdout: ""},
	})
	if _, err := moduleUdmUser(context.Background(), conn, map[string]any{"username": "foo"}); err == nil {
		t.Fatal("want error for missing firstname/lastname/password")
	}
}

func TestModuleUdmUserCreate(t *testing.T) {
	findCmd := "udm users/user list --filter username=foo"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:             {RC: 0, Stdout: ""},
	})
	res, err := moduleUdmUser(context.Background(), conn, map[string]any{
		"username": "foo", "firstname": "Foo", "lastname": "Bar", "password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	var createCmd string
	for _, c := range conn.Commands {
		if strings.HasPrefix(c, "udm users/user create ") {
			createCmd = c
		}
	}
	if createCmd == "" {
		t.Fatalf("no create command found among %v", conn.Commands)
	}
	for _, want := range []string{
		"--position cn=users,dc=example,dc=com",
		"--set username=foo",
		"--set firstname=Foo",
		"--set lastname=Bar",
		"--set password=secret",
		"--set 'displayName=Foo Bar'",
		"--set unixhome=/home/foo",
		"--set shell=/bin/bash",
	} {
		if !strings.Contains(createCmd, want) {
			t.Errorf("create command %q missing %q", createCmd, want)
		}
	}
}

func TestModuleUdmUserGroupSync(t *testing.T) {
	findUserCmd := "udm users/user list --filter username=foo"
	findGroupCmd := "udm groups/group list --filter name=staff"
	groupListOut := "DN: cn=staff,cn=groups,dc=example,dc=com\n  name: staff\n"
	appendCmd := "udm groups/group modify --dn cn=staff,cn=groups,dc=example,dc=com --append users=uid=foo,cn=users,dc=example,dc=com"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findUserCmd:         {RC: 0, Stdout: ""},
		findGroupCmd:        {RC: 0, Stdout: groupListOut},
		appendCmd:           {RC: 0},
	})
	res, err := moduleUdmUser(context.Background(), conn, map[string]any{
		"username": "foo", "firstname": "Foo", "lastname": "Bar", "password": "secret",
		"groups": []any{"staff"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == appendCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want group append", conn.Commands)
	}
}

func TestModuleUdmUserAbsentAlready(t *testing.T) {
	findCmd := "udm users/user list --filter username=foo"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:             {RC: 0, Stdout: ""},
	})
	res, err := moduleUdmUser(context.Background(), conn, map[string]any{
		"username": "foo", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}
