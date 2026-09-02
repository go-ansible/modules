package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func mustLocalScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "myscript.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModuleScriptRuns(t *testing.T) {
	local := mustLocalScript(t)
	remote := "/tmp/myscript.sh"
	conn := newFakeConn(map[string]remoteexec.Result{
		shellQuote(remote): {RC: 0, Stdout: "hi\n"},
	})
	res, err := moduleScript(context.Background(), conn, map[string]any{"cmd": local})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScriptWithArgsAndChdir(t *testing.T) {
	local := mustLocalScript(t)
	remote := "/tmp/myscript.sh"
	cmd := "cd /opt && " + shellQuote(remote) + " foo bar"
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0},
	})
	res, err := moduleScript(context.Background(), conn, map[string]any{
		"cmd": local + " foo bar", "chdir": "/opt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScriptWithExecutable(t *testing.T) {
	local := mustLocalScript(t)
	remote := "/tmp/myscript.sh"
	cmd := shellQuote("/usr/bin/python3") + " " + shellQuote(remote)
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0},
	})
	res, err := moduleScript(context.Background(), conn, map[string]any{
		"cmd": local, "executable": "/usr/bin/python3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScriptCreatesSkip(t *testing.T) {
	local := mustLocalScript(t)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote("/marker"): {RC: 0},
	})
	res, err := moduleScript(context.Background(), conn, map[string]any{"cmd": local, "creates": "/marker"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged (skipped)")
	}
}

func TestModuleScriptMissingCmd(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleScript(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleScriptPutError(t *testing.T) {
	local := mustLocalScript(t)
	conn := &failAfterConn{n: 0}
	if _, err := moduleScript(context.Background(), conn, map[string]any{"cmd": local}); err == nil {
		t.Fatal("want error propagated from Put")
	}
}
