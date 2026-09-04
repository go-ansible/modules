package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUdmShareMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUdmShare(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name/ou")
	}
}

func TestModuleUdmShareRequiresPathHostSambaName(t *testing.T) {
	findCmd := "udm shares/share list --filter name=home"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:             {RC: 0, Stdout: ""},
	})
	if _, err := moduleUdmShare(context.Background(), conn, map[string]any{
		"name": "home", "ou": "school",
	}); err == nil {
		t.Fatal("want error for missing path/host/sambaName")
	}
}

func TestModuleUdmShareCreate(t *testing.T) {
	findCmd := "udm shares/share list --filter name=home"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:             {RC: 0, Stdout: ""},
	})
	res, err := moduleUdmShare(context.Background(), conn, map[string]any{
		"name": "home", "ou": "school", "path": "/home",
		"host": "ucs.example.com", "sambaName": "Home",
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
		if strings.HasPrefix(c, "udm shares/share create ") {
			createCmd = c
		}
	}
	if createCmd == "" {
		t.Fatalf("no create command found among %v", conn.Commands)
	}
	for _, want := range []string{
		"--position cn=shares,ou=school,dc=example,dc=com",
		"--set name=home",
		"--set path=/home",
		"--set host=ucs.example.com",
		"--set sambaName=Home",
		"--set 'printablename=home (ucs.example.com)'",
		"--set sambaBrowseable=1",
		"--set writeable=1",
	} {
		if !strings.Contains(createCmd, want) {
			t.Errorf("create command %q missing %q", createCmd, want)
		}
	}
}

func TestModuleUdmShareAbsentAlready(t *testing.T) {
	findCmd := "udm shares/share list --filter name=home"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:             {RC: 0, Stdout: ""},
	})
	res, err := moduleUdmShare(context.Background(), conn, map[string]any{
		"name": "home", "ou": "school", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleUdmShareCustomSettings(t *testing.T) {
	out := udmShareCustomSettings(map[string]any{
		"sambaCustomSettings": []any{
			map[string]any{"key": "vfs objects", "value": "recycle"},
		},
	})
	if len(out) != 1 || out[0] != "vfs objects recycle" {
		t.Fatalf("udmShareCustomSettings = %v", out)
	}
}
