package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func riakStatsCurlCmd(httpConn string) string {
	return "curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}") + " " + shellQuote("http://"+httpConn+"/stats")
}

const riakStatsBody = `{"nodename":"riak@127.0.0.1","ring_members":["riak@127.0.0.1"],"ring_creation_size":64}`

func riakBaseCommands(t *testing.T, adminFound bool) map[string]remoteexec.Result {
	t.Helper()
	adminCheckRC := 0
	if !adminFound {
		adminCheckRC = 1
	}
	return map[string]remoteexec.Result{
		"command -v riak-admin":            {RC: adminCheckRC},
		riakStatsCurlCmd("127.0.0.1:8098"): {RC: 0, Stdout: riakStatsBody + "\nHTTPSTATUS:200"},
		"riak version":                     {RC: 0, Stdout: "3.0.0\n"},
		"riak-admin ringready":             {RC: 0, Stdout: "TRUE All nodes agree on the ring\n"},
		"riak admin ringready":             {RC: 0, Stdout: "TRUE All nodes agree on the ring\n"},
	}
}

func TestModuleRiakFactsOnly(t *testing.T) {
	conn := newFakeConn(riakBaseCommands(t, true))
	res, err := moduleRiak(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["node_name"] != "riak@127.0.0.1" {
		t.Fatalf("node_name = %v", res.Extra["node_name"])
	}
	if res.Extra["ring_ready"] != true {
		t.Fatalf("ring_ready = %v", res.Extra["ring_ready"])
	}
	if res.Extra["version"] != "3.0.0" {
		t.Fatalf("version = %v", res.Extra["version"])
	}
}

func TestModuleRiakAdminFallback(t *testing.T) {
	cmds := riakBaseCommands(t, false)
	conn := newFakeConn(cmds)
	res, err := moduleRiak(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "riak admin ringready" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want fallback to 'riak admin' when riak-admin is not on PATH", conn.Commands)
	}
}

func TestModuleRiakPing(t *testing.T) {
	cmds := riakBaseCommands(t, true)
	cmds["riak ping riak@127.0.0.1"] = remoteexec.Result{RC: 0, Stdout: "pong\n"}
	conn := newFakeConn(cmds)
	res, err := moduleRiak(context.Background(), conn, map[string]any{"command": "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Extra["ping"] != "pong\n" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRiakPingFailure(t *testing.T) {
	cmds := riakBaseCommands(t, true)
	cmds["riak ping riak@127.0.0.1"] = remoteexec.Result{RC: 1, Stdout: "unable to connect\n"}
	conn := newFakeConn(cmds)
	res, err := moduleRiak(context.Background(), conn, map[string]any{"command": "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRiakJoinAlreadyInCluster(t *testing.T) {
	cmds := riakBaseCommands(t, true)
	cmds[riakStatsCurlCmd("127.0.0.1:8098")] = remoteexec.Result{
		RC: 0,
		Stdout: `{"nodename":"riak@127.0.0.1","ring_members":["riak@127.0.0.1","riak@10.1.1.2"],"ring_creation_size":64}` +
			"\nHTTPSTATUS:200",
	}
	conn := newFakeConn(cmds)
	res, err := moduleRiak(context.Background(), conn, map[string]any{"command": "join"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["join"] != "Node is already in cluster or staged to be in cluster." {
		t.Fatalf("join = %v", res.Extra["join"])
	}
}

func TestModuleRiakCommit(t *testing.T) {
	cmds := riakBaseCommands(t, true)
	cmds["riak-admin cluster commit"] = remoteexec.Result{RC: 0, Stdout: "Cluster changes committed\n"}
	conn := newFakeConn(cmds)
	res, err := moduleRiak(context.Background(), conn, map[string]any{"command": "commit"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRiakInvalidCommand(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRiak(context.Background(), conn, map[string]any{"command": "bogus"}); err == nil {
		t.Fatal("want error for invalid command")
	}
}
