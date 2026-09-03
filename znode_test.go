package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const znodeCmd = "zkCli.sh -server localhost:2181"

func TestModuleZnodeMissingHosts(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZnode(context.Background(), conn, map[string]any{"name": "/x"}); err == nil {
		t.Fatal("want error for missing hosts")
	}
}

func TestModuleZnodeStateAndOpMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZnode(context.Background(), conn, map[string]any{
		"hosts": "localhost:2181", "name": "/x", "state": "present", "op": "get",
	}); err == nil {
		t.Fatal("want error: state and op are mutually exclusive")
	}
}

func TestModuleZnodeCreateNew(t *testing.T) {
	// The create call itself needs its own successful result; since
	// fakeConn is keyed purely by command text (identical for every
	// zkCli invocation here), script it to succeed after the first
	// "stat" probe by using a sequenced connection instead.
	seq := &sequencedFakeConn{fakeConn: newFakeConn(nil), script: []scriptedExec{
		{cmd: znodeCmd, result: remoteexec.Result{RC: 1, Stdout: "Node does not exist: /mypath"}}, // stat /mypath
		{cmd: znodeCmd, result: remoteexec.Result{RC: 0}},                                         // create /mypath
	}}
	res, err := moduleZnode(context.Background(), seq, map[string]any{
		"hosts": "localhost:2181", "name": "/mypath", "value": "myvalue", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if len(seq.fakeConn.Commands) != 2 {
		t.Fatalf("commands = %v, want 2 zkCli invocations (stat, then create)", seq.fakeConn.Commands)
	}
}

func TestModuleZnodeAlreadySet(t *testing.T) {
	seq := &sequencedFakeConn{fakeConn: newFakeConn(nil), script: []scriptedExec{
		{cmd: znodeCmd, result: remoteexec.Result{RC: 0, Stdout: "cZxid = 0x1\n"}},                       // stat: exists
		{cmd: znodeCmd, result: remoteexec.Result{RC: 0, Stdout: "myvalue\ncZxid = 0x1\nctime = 123\n"}}, // get
	}}
	res, err := moduleZnode(context.Background(), seq, map[string]any{
		"hosts": "localhost:2181", "name": "/mypath", "value": "myvalue", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleZnodeAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		znodeCmd: {RC: 1, Stdout: "Node does not exist: /mypath"},
	})
	res, err := moduleZnode(context.Background(), conn, map[string]any{
		"hosts": "localhost:2181", "name": "/mypath", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleZnodeAbsentDeletes(t *testing.T) {
	seq := &sequencedFakeConn{fakeConn: newFakeConn(nil), script: []scriptedExec{
		{cmd: znodeCmd, result: remoteexec.Result{RC: 0, Stdout: "cZxid = 0x1\n"}}, // stat: exists
		{cmd: znodeCmd, result: remoteexec.Result{RC: 0}},                          // delete
	}}
	res, err := moduleZnode(context.Background(), seq, map[string]any{
		"hosts": "localhost:2181", "name": "/mypath", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(seq.fakeConn.Commands) != 2 {
		t.Fatalf("commands = %v, want 2 zkCli invocations (stat, then delete)", seq.fakeConn.Commands)
	}
}

func TestModuleZnodeOpGet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		znodeCmd: {RC: 0, Stdout: "myvalue\ncZxid = 0x1\nctime = 123\n"},
	})
	res, err := moduleZnode(context.Background(), conn, map[string]any{
		"hosts": "localhost:2181", "name": "/mypath", "op": "get",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "myvalue" {
		t.Fatalf("msg = %q", res.Msg)
	}
	stat := res.Extra["stat"].(map[string]any)
	if stat["cZxid"] != "0x1" {
		t.Fatalf("stat = %v", stat)
	}
}

func TestModuleZnodeOpList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		znodeCmd: {RC: 0, Stdout: "[zookeeper, myapp]\n"},
	})
	res, err := moduleZnode(context.Background(), conn, map[string]any{
		"hosts": "localhost:2181", "name": "/", "op": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	children := res.Extra["children"].([]string)
	if len(children) != 2 || children[0] != "zookeeper" || children[1] != "myapp" {
		t.Fatalf("children = %v", children)
	}
}

func TestModuleZnodeOpWaitFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		znodeCmd: {RC: 0, Stdout: "cZxid = 0x1\n"},
	})
	res, err := moduleZnode(context.Background(), conn, map[string]any{
		"hosts": "localhost:2181", "name": "/mypath", "op": "wait", "timeout": 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZnodeAuthCredentialPrepended(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		znodeCmd: {RC: 1, Stdout: "Node does not exist: /mypath"},
	})
	if _, err := moduleZnode(context.Background(), conn, map[string]any{
		"hosts": "localhost:2181", "name": "/mypath", "op": "get", "auth_credential": "user1:s3cr3t",
	}); err != nil {
		t.Fatal(err)
	}
	if len(conn.Stdins) == 0 || !strings.HasPrefix(conn.Stdins[0], "addauth digest user1:s3cr3t\n") {
		t.Fatalf("stdins = %v", conn.Stdins)
	}
}
