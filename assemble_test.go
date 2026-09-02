package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestAssembleCmd(t *testing.T) {
	cmd := assembleCmd("/frags", "/out", "", "")
	want := `find /frags -mindepth 1 -maxdepth 1 -type f | sort | while IFS= read -r f; do cat "$f"; done > /out`
	if cmd != want {
		t.Fatalf("cmd = %q, want %q", cmd, want)
	}

	cmd = assembleCmd("/frags", "/out", `^\d+-`, "\n")
	if cmd == want {
		t.Fatal("regexp/delimiter should change the command")
	}
}

func TestModuleAssembleSuccess(t *testing.T) {
	cmd := assembleCmd("/frags", "/out", "", "")
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0},
	})
	res, err := moduleAssemble(context.Background(), conn, map[string]any{"src": "/frags", "dest": "/out"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAssembleFailure(t *testing.T) {
	cmd := assembleCmd("/frags", "/out", "", "")
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 1, Stderr: "no such directory"},
	})
	res, err := moduleAssemble(context.Background(), conn, map[string]any{"src": "/frags", "dest": "/out"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}

func TestModuleAssembleWithRegexpAndDelimiter(t *testing.T) {
	cmd := assembleCmd("/frags", "/out", `^\d+`, "---")
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0},
	})
	res, err := moduleAssemble(context.Background(), conn, map[string]any{
		"src": "/frags", "dest": "/out", "regexp": `^\d+`, "delimiter": "---",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAssembleMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAssemble(context.Background(), conn, map[string]any{"dest": "/out"}); err == nil {
		t.Fatal("want error for missing src")
	}
	if _, err := moduleAssemble(context.Background(), conn, map[string]any{"src": "/frags"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}
