package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// newLxdSequencedConn wraps the package's own sequencedFakeConn (defined
// in haproxy_test.go) to return results in call order — needed here
// because lxd_container.go queries the SAME `lxc list` command twice
// (before and after creation) expecting a different answer each time,
// which plain fakeConn's command-keyed map cannot express.
func newLxdSequencedConn(results ...remoteexec.Result) *sequencedFakeConn {
	script := make([]scriptedExec, len(results))
	for i, r := range results {
		script[i] = scriptedExec{result: r}
	}
	return &sequencedFakeConn{fakeConn: newFakeConn(nil), script: script}
}

func TestModuleLxdContainerMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxdContainer(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleLxdContainerInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxdContainer(context.Background(), conn, map[string]any{
		"name": "x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleLxdContainerCreateStartedFull(t *testing.T) {
	name := "mycontainer"
	conn := newLxdSequencedConn(
		remoteexec.Result{RC: 0, Stdout: "[]"}, // initial lxc list: not found
		remoteexec.Result{RC: 0},               // lxc init
		remoteexec.Result{RC: 0, Stdout: `[{"name":"mycontainer","status":"Stopped","type":"container","profiles":["default"],"config":{},"devices":{}}]`}, // lxc list after create
		remoteexec.Result{RC: 0}, // lxc start
	)
	res, err := moduleLxdContainer(context.Background(), conn, map[string]any{
		"name":  name,
		"state": "started",
		"source": map[string]any{
			"type":  "image",
			"alias": "ubuntu:22.04",
		},
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
	actions, _ := res.Extra["actions"].([]string)
	want := []string{"create", "start"}
	if len(actions) != len(want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("actions = %v, want %v", actions, want)
		}
	}
	if conn.fakeConn.Commands[1] != "lxc init ubuntu:22.04 "+name {
		t.Fatalf("create command = %q", conn.fakeConn.Commands[1])
	}
}

func TestModuleLxdContainerMissingSourceAlias(t *testing.T) {
	conn := newLxdSequencedConn(remoteexec.Result{RC: 0, Stdout: "[]"})
	if _, err := moduleLxdContainer(context.Background(), conn, map[string]any{
		"name": "x", "state": "started",
	}); err == nil {
		t.Fatal("want error: source.alias required to create")
	}
}

func TestModuleLxdContainerAlreadyRunningUnchanged(t *testing.T) {
	name := "mycontainer"
	listCmd := "lxc list " + name + " --format json"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: `[{"name":"mycontainer","status":"Running","type":"container","profiles":["default"],"config":{},"devices":{}}]`},
	})
	res, err := moduleLxdContainer(context.Background(), conn, map[string]any{
		"name":  name,
		"state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	if res.Extra["old_state"] != "running" {
		t.Fatalf("old_state = %v", res.Extra["old_state"])
	}
}

func TestModuleLxdContainerConfigReconcile(t *testing.T) {
	name := "mycontainer"
	listCmd := "lxc list " + name + " --format json"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: `[{"name":"mycontainer","status":"Running","type":"container","profiles":["default"],"config":{"limits.cpu":"1"},"devices":{}}]`},
		"lxc config set " + name + " limits.cpu 2": {RC: 0},
	})
	res, err := moduleLxdContainer(context.Background(), conn, map[string]any{
		"name":   name,
		"state":  "started",
		"config": map[string]any{"limits.cpu": "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "lxc config set "+name+" limits.cpu 2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want a config set call", conn.Commands)
	}
}

func TestModuleLxdContainerAbsentAlready(t *testing.T) {
	name := "gone"
	listCmd := "lxc list " + name + " --format json"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: "[]"},
	})
	res, err := moduleLxdContainer(context.Background(), conn, map[string]any{
		"name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleLxdContainerAbsentDeletes(t *testing.T) {
	name := "mycontainer"
	listCmd := "lxc list " + name + " --format json"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd:                           {RC: 0, Stdout: `[{"name":"mycontainer","status":"Running","type":"container","profiles":[],"config":{},"devices":{}}]`},
		"lxc delete " + name + " --force": {RC: 0},
	})
	res, err := moduleLxdContainer(context.Background(), conn, map[string]any{
		"name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
