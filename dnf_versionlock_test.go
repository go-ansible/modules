package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDnfVersionlockPresentAdds(t *testing.T) {
	// The same "dnf -q versionlock list" command is used both for the
	// pre-check and (since a change was made) the locklist_post
	// re-query; the fake connection is scripted per exact command
	// string, so both queries return the same (pre-change) fake output.
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf -q versionlock list":      {RC: 0, Stdout: ""},
		"dnf -q versionlock add nginx": {RC: 0},
	})
	res, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"name": "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if got := res.Extra["specs_toadd"].([]string); len(got) != 1 || got[0] != "nginx" {
		t.Fatalf("specs_toadd = %v", got)
	}
}

func TestModuleDnfVersionlockAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf -q versionlock list": {RC: 0, Stdout: "nginx-0:1.20.1-1.el8.*\n"},
	})
	res, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"name": "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want only the list query (no locklist_post re-query, no add)", conn.Commands)
	}
}

func TestModuleDnfVersionlockExcluded(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf -q versionlock list":         {RC: 0, Stdout: ""},
		"dnf -q versionlock exclude bind": {RC: 0},
	})
	res, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"name": "bind", "state": "excluded"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnfVersionlockRaw(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf -q versionlock list":                 {RC: 0, Stdout: ""},
		"dnf -q versionlock add --raw bash-0:4.*": {RC: 0},
	})
	res, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{
		"name": "bash-0:4.*", "raw": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnfVersionlockAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf -q versionlock list":         {RC: 0, Stdout: "nginx-0:1.20.1-1.el8.*\n"},
		"dnf -q versionlock delete nginx": {RC: 0},
	})
	res, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"name": "nginx", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnfVersionlockAbsentNotPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf -q versionlock list": {RC: 0, Stdout: ""},
	})
	res, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"name": "nginx", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleDnfVersionlockClean(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf -q versionlock list":  {RC: 0, Stdout: "nginx-0:1.20.1-1.el8.*\nhttpd-0:2.4.37-1.el8.*\n"},
		"dnf -q versionlock clear": {RC: 0},
	})
	res, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"state": "clean"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDnfVersionlockCleanAlreadyEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf -q versionlock list": {RC: 0, Stdout: ""},
	})
	res, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"state": "clean"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleDnfVersionlockCleanMutuallyExclusiveWithName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"state": "clean", "name": "nginx"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleDnfVersionlockNameRequired(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: name required for state=present")
	}
}

func TestModuleDnfVersionlockInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDnfVersionlock(context.Background(), conn, map[string]any{"name": "nginx", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}
