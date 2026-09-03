package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLxcContainerMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxcContainer(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleLxcContainerInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": "x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleLxcContainerContainerLogRejected(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": "x", "container_log": true,
	}); err == nil {
		t.Fatal("want error: container_log not supported")
	}
}

func TestModuleLxcContainerCreateAndStart(t *testing.T) {
	name := "mycontainer"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc-info -n " + name + " -s":                 {RC: 1},
		"lxc-create -n " + name + " -t ubuntu -B dir": {RC: 0},
		"lxc-start -n " + name + " -d":                {RC: 0},
	})
	res, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": name, "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	actions := res.Extra["actions"].([]string)
	want := []string{"create", "start"}
	if len(actions) != len(want) || actions[0] != want[0] || actions[1] != want[1] {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
}

func TestModuleLxcContainerAlreadyRunningUnchanged(t *testing.T) {
	name := "mycontainer"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc-info -n " + name + " -s": {RC: 0, Stdout: "State: RUNNING\nPID: 123\n"},
	})
	res, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": name, "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLxcContainerAbsentAlready(t *testing.T) {
	name := "gone"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc-info -n " + name + " -s": {RC: 1},
	})
	res, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleLxcContainerAbsentDestroys(t *testing.T) {
	name := "mycontainer"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc-info -n " + name + " -s":    {RC: 0, Stdout: "State: RUNNING\n"},
		"lxc-destroy -n " + name + " -f": {RC: 0},
	})
	res, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleLxcContainerFreeze(t *testing.T) {
	name := "mycontainer"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc-info -n " + name + " -s": {RC: 0, Stdout: "State: RUNNING\n"},
		"lxc-freeze -n " + name:       {RC: 0},
	})
	res, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": name, "state": "frozen",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	actions := res.Extra["actions"].([]string)
	if len(actions) != 1 || actions[0] != "freeze" {
		t.Fatalf("actions = %v", actions)
	}
}

func TestModuleLxcContainerCloneRequiresCloneName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": "src", "state": "clone",
	}); err == nil {
		t.Fatal("want error: clone_name required")
	}
}

func TestModuleLxcContainerClone(t *testing.T) {
	name, cloneName := "src", "dst"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc-info -n " + cloneName + " -s":          {RC: 1},
		"lxc-clone -o " + name + " -n " + cloneName: {RC: 0},
	})
	res, err := moduleLxcContainer(context.Background(), conn, map[string]any{
		"name": name, "state": "clone", "clone_name": cloneName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
