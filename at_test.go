package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAtScheduleCommand(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"at now + 20 minutes": {RC: 0},
	})
	res, err := moduleAt(context.Background(), conn, map[string]any{
		"command": "ls -d /", "count": 20, "units": "minutes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Stdins[len(conn.Stdins)-1] != "ls -d /\n" {
		t.Fatalf("stdin = %q", conn.Stdins[len(conn.Stdins)-1])
	}
}

func TestModuleAtScheduleScriptFile(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"at -f /tmp/script.sh now + 1 hours": {RC: 0},
	})
	res, err := moduleAt(context.Background(), conn, map[string]any{
		"script_file": "/tmp/script.sh", "count": 1, "units": "hours",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAtUniqueAlreadyScheduled(t *testing.T) {
	findCmd := `for j in $(atq 2>/dev/null | awk '{print $1}'); do if at -c "$j" 2>/dev/null | grep -qF -- 'ls -d /'; then echo "$j"; fi; done`
	conn := newFakeConn(map[string]remoteexec.Result{
		findCmd: {RC: 0, Stdout: "3\n"},
	})
	res, err := moduleAt(context.Background(), conn, map[string]any{
		"command": "ls -d /", "count": 20, "units": "minutes", "unique": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: a matching job already exists")
	}
}

func TestModuleAtAbsent(t *testing.T) {
	findCmd := `for j in $(atq 2>/dev/null | awk '{print $1}'); do if at -c "$j" 2>/dev/null | grep -qF -- 'ls -d /'; then echo "$j"; fi; done`
	conn := newFakeConn(map[string]remoteexec.Result{
		findCmd:    {RC: 0, Stdout: "3\n5\n"},
		"atrm 3 5": {RC: 0},
	})
	res, err := moduleAt(context.Background(), conn, map[string]any{
		"command": "ls -d /", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAtAbsentNoMatch(t *testing.T) {
	findCmd := `for j in $(atq 2>/dev/null | awk '{print $1}'); do if at -c "$j" 2>/dev/null | grep -qF -- 'ls -d /'; then echo "$j"; fi; done`
	conn := newFakeConn(map[string]remoteexec.Result{
		findCmd: {RC: 0, Stdout: ""},
	})
	res, err := moduleAt(context.Background(), conn, map[string]any{
		"command": "ls -d /", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleAtValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAt(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: neither command nor script_file")
	}
	if _, err := moduleAt(context.Background(), conn, map[string]any{"command": "x", "script_file": "y"}); err == nil {
		t.Fatal("want error: mutually exclusive")
	}
	if _, err := moduleAt(context.Background(), conn, map[string]any{"command": "x"}); err == nil {
		t.Fatal("want error: missing count/units")
	}
	if _, err := moduleAt(context.Background(), conn, map[string]any{"script_file": "x", "state": "absent"}); err == nil {
		t.Fatal("want error: absent requires command")
	}
	if _, err := moduleAt(context.Background(), conn, map[string]any{"command": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error: bad state")
	}
}
