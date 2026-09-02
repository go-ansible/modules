package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePingDefault(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		":": {RC: 0},
	})
	res, err := modulePing(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Msg != "pong" {
		t.Fatalf("msg = %q", res.Msg)
	}
	if res.Extra["ping"] != "pong" {
		t.Fatalf("ping = %v", res.Extra["ping"])
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != ":" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModulePingCustomData(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		":": {RC: 0},
	})
	res, err := modulePing(context.Background(), conn, map[string]any{"data": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["ping"] != "hello" {
		t.Fatalf("ping = %v", res.Extra["ping"])
	}
}

func TestModulePingCrash(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := modulePing(context.Background(), conn, map[string]any{"data": "crash"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for data=crash")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no exec when crashing before it, commands = %v", conn.Commands)
	}
}

func TestModulePingConnError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		":": {RC: 1, Stderr: "no"},
	})
	_, err := modulePing(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error when the no-op exec fails")
	}
}
