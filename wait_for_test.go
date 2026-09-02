package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestWaitForScriptValidation(t *testing.T) {
	if _, err := waitForScript("h", 0, "", 1, 0, "bogus"); err == nil {
		t.Fatal("want error for invalid state")
	}
	if _, err := waitForScript("h", 22, "/p", 1, 0, "started"); err == nil {
		t.Fatal("want error when both port and path are given")
	}
}

func TestWaitForScriptTimeoutOnly(t *testing.T) {
	cmd, err := waitForScript("127.0.0.1", 0, "", 30, 0, "started")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "sleep 30" {
		t.Fatalf("cmd = %q", cmd)
	}
}

func TestWaitForScriptPortAndPathShape(t *testing.T) {
	cmd, err := waitForScript("localhost", 22, "", 5, 0, "started")
	if err != nil {
		t.Fatal(err)
	}
	if cmd == "" || cmd[:8] != "bash -c " {
		t.Fatalf("cmd = %q, want a bash -c invocation", cmd)
	}

	cmd2, err := waitForScript("localhost", 0, "/tmp/f", 5, 0, "absent")
	if err != nil {
		t.Fatal(err)
	}
	if cmd2 == cmd {
		t.Fatal("port and path scripts should differ")
	}
}

func TestModuleWaitForPortReachedImmediately(t *testing.T) {
	cmd, err := waitForScript("127.0.0.1", 22, "", 5, 0, "started")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{cmd: {RC: 0}})
	res, err := moduleWaitFor(context.Background(), conn, map[string]any{"port": 22})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleWaitForTimesOut(t *testing.T) {
	cmd, err := waitForScript("127.0.0.1", 22, "", 5, 0, "started")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{cmd: {RC: 1}})
	res, err := moduleWaitFor(context.Background(), conn, map[string]any{"port": 22, "timeout": 5})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed on timeout")
	}
}

func TestModuleWaitForPath(t *testing.T) {
	cmd, err := waitForScript("127.0.0.1", 0, "/tmp/ready", 300, 0, "present")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{cmd: {RC: 0}})
	res, err := moduleWaitFor(context.Background(), conn, map[string]any{"path": "/tmp/ready", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleWaitForTimeoutOnlySleeps(t *testing.T) {
	cmd, err := waitForScript("127.0.0.1", 0, "", 1, 0, "started")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{cmd: {RC: 0}})
	res, err := moduleWaitFor(context.Background(), conn, map[string]any{"timeout": 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleWaitForInvalidArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleWaitFor(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
