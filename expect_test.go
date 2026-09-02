package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestExpectStdin(t *testing.T) {
	if s := expectStdin(map[string]any{}); s != "" {
		t.Fatalf("expectStdin(empty) = %q", s)
	}
	s := expectStdin(map[string]any{"responses": map[string]any{"Password:": "secret"}})
	if s != "secret\n" {
		t.Fatalf("expectStdin = %q", s)
	}
	s = expectStdin(map[string]any{"responses": map[string]any{
		"a": "1",
		"b": []any{"2", "3"},
	}})
	if s != "1\n2\n" {
		t.Fatalf("expectStdin(sorted) = %q", s)
	}
}

func TestModuleExpectRunsCommandWithStdin(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"myprog": {RC: 0, Stdout: "done"},
	})
	res, err := moduleExpect(context.Background(), conn, map[string]any{
		"command":   "myprog",
		"responses": map[string]any{"Continue?": "yes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Stdins) != 1 || conn.Stdins[0] != "yes\n" {
		t.Fatalf("Stdins = %v", conn.Stdins)
	}
}

func TestModuleExpectNoResponses(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"myprog": {RC: 0},
	})
	res, err := moduleExpect(context.Background(), conn, map[string]any{"command": "myprog"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if conn.Stdins[0] != "" {
		t.Fatalf("Stdins = %v, want empty", conn.Stdins)
	}
}

func TestModuleExpectChdir(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cd /tmp && myprog": {RC: 0},
	})
	res, err := moduleExpect(context.Background(), conn, map[string]any{"command": "myprog", "chdir": "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleExpectCreatesSkip(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote("/marker"): {RC: 0},
	})
	res, err := moduleExpect(context.Background(), conn, map[string]any{"command": "myprog", "creates": "/marker"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged (skipped)")
	}
}

func TestModuleExpectMissingCommand(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleExpect(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
