package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAixInittabAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsitab startmyservice": {RC: 1},
		`mkitab -i existingservice 'startmyservice:4:once:echo hello'`: {RC: 0},
	})
	res, err := moduleAixInittab(context.Background(), conn, map[string]any{
		"name": "startmyservice", "runlevel": "4", "action": "once", "command": "echo hello",
		"insertafter": "existingservice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixInittabChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsitab startmyservice":                     {RC: 0, Stdout: "startmyservice:4:once:echo hello\n"},
		`chitab 'startmyservice:2:wait:echo hello'`: {RC: 0},
	})
	res, err := moduleAixInittab(context.Background(), conn, map[string]any{
		"name": "startmyservice", "runlevel": "2", "action": "wait", "command": "echo hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixInittabUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsitab startmyservice": {RC: 0, Stdout: "startmyservice:4:once:echo hello\n"},
	})
	res, err := moduleAixInittab(context.Background(), conn, map[string]any{
		"name": "startmyservice", "runlevel": "4", "action": "once", "command": "echo hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixInittabRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsitab startmyservice": {RC: 0, Stdout: "startmyservice:2:wait:echo hello\n"},
		"rmitab startmyservice": {RC: 0},
	})
	res, err := moduleAixInittab(context.Background(), conn, map[string]any{
		"name": "startmyservice", "runlevel": "2", "action": "wait", "command": "echo hello", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixInittabRemoveAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsitab startmyservice": {RC: 1},
	})
	res, err := moduleAixInittab(context.Background(), conn, map[string]any{
		"name": "startmyservice", "runlevel": "2", "action": "wait", "command": "echo hello", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixInittabMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAixInittab(context.Background(), conn, map[string]any{"name": "x", "command": "echo hi"}); err == nil {
		t.Fatal("want error for missing runlevel")
	}
}
