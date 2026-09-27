package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRawRunsDirectly(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"echo hi | cat": {RC: 0, Stdout: "hi\n"},
	})
	res, err := moduleRaw(context.Background(), conn, map[string]any{"cmd": "echo hi | cat"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["stdout"] != "hi\n" {
		t.Fatalf("stdout = %v", res.Extra["stdout"])
	}
}

func TestModuleRawUsesRawParams(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uptime": {RC: 0},
	})
	res, err := moduleRaw(context.Background(), conn, map[string]any{"_raw_params": "uptime"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRawNonZeroExit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"false": {RC: 1},
	})
	res, err := moduleRaw(context.Background(), conn, map[string]any{"cmd": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}

func TestModuleRawMissingCmd(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRaw(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

// Measured from real ansible-core 2.21.4: raw and script share a result
// shape that is NOT command's.
//
//	command/shell: changed cmd delta end failed msg rc start
//	               stderr stderr_lines stdout stdout_lines
//	raw/script:    changed failed rc stderr stderr_lines stdout
//	               stdout_lines   (+ msg and exception on failure)
func TestRawResultShape(t *testing.T) {
	ok := rawResult(remoteexec.Result{RC: 0, Stdout: "out", Stderr: ""})
	if ok.Failed {
		t.Error("rc 0 reported as failed")
	}
	if !ok.NoMsg {
		t.Error("a successful raw carries a msg key; real has none")
	}
	if _, has := ok.Extra["cmd"]; has {
		t.Error("raw reported a cmd key; real has none")
	}
	for _, k := range []string{"delta", "end", "start"} {
		if _, has := ok.Extra[k]; has {
			t.Errorf("raw reported %q; real has no timing keys", k)
		}
	}
	for _, k := range []string{"stdout", "stderr", "rc"} {
		if _, has := ok.Extra[k]; !has {
			t.Errorf("raw did not report %q", k)
		}
	}

	bad := rawResult(remoteexec.Result{RC: 5})
	if !bad.Failed {
		t.Error("rc 5 not reported as failed")
	}
	if bad.NoMsg {
		t.Error("a failing raw has no msg; real reports one")
	}
	// Real's own wording, and NOT command's "The command exited with a
	// non-zero return code."
	if bad.Msg != "non-zero return code" {
		t.Errorf("msg = %q, want %q", bad.Msg, "non-zero return code")
	}
}
