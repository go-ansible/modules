package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUdmGroupMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUdmGroup(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleUdmGroupCreate(t *testing.T) {
	findCmd := "udm groups/group list --filter name=g123m-1A"
	createCmd := "udm groups/group create --position cn=groups,dc=example,dc=com --set name=g123m-1A"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: ""},
		createCmd:            {RC: 0},
	})
	res, err := moduleUdmGroup(context.Background(), conn, map[string]any{"name": "g123m-1A"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleUdmGroupAlreadyUpToDate(t *testing.T) {
	findCmd := "udm groups/group list --filter name=g123m-1A"
	listOut := "DN: cn=g123m-1A,cn=groups,dc=example,dc=com\n  name: g123m-1A\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: listOut},
	})
	res, err := moduleUdmGroup(context.Background(), conn, map[string]any{"name": "g123m-1A"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleUdmGroupWithPositionOverride(t *testing.T) {
	position := "cn=classes,cn=students,cn=groups,ou=school,dc=school,dc=example,dc=com"
	findCmd := "udm groups/group list --filter name=g123m-1A"
	createCmd := "udm groups/group create --position " + position + " --set name=g123m-1A"
	conn := newFakeConn(map[string]remoteexec.Result{
		findCmd:   {RC: 0, Stdout: ""},
		createCmd: {RC: 0},
	})
	res, err := moduleUdmGroup(context.Background(), conn, map[string]any{
		"name": "g123m-1A", "position": position,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	// position override should skip the ucr lookup entirely.
	for _, c := range conn.Commands {
		if c == "ucr get ldap/base" {
			t.Fatal("want no ucr lookup when position is set explicitly")
		}
	}
}

func TestModuleUdmGroupAbsentRemoves(t *testing.T) {
	findCmd := "udm groups/group list --filter name=g123m-1A"
	listOut := "DN: cn=g123m-1A,cn=groups,dc=example,dc=com\n  name: g123m-1A\n"
	removeCmd := "udm groups/group remove --dn cn=g123m-1A,cn=groups,dc=example,dc=com"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: listOut},
		removeCmd:            {RC: 0},
	})
	res, err := moduleUdmGroup(context.Background(), conn, map[string]any{
		"name": "g123m-1A", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
