package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMemsetDNSReloadNoPoll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":           {RC: 0},
		"ma-shell -k AAAAAA dns.reload": {RC: 0, Stdout: `{"id":"job1","finished":false,"error":false,"status":"NEW","type":"dns"}`},
	})
	res, err := moduleMemsetDNSReload(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	meta, _ := res.Extra["memset_api"].(map[string]any)
	if meta["id"] != "job1" {
		t.Fatalf("meta = %+v", res.Extra["memset_api"])
	}
}

func TestModuleMemsetDNSReloadPollFinishesImmediately(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                   {RC: 0},
		"ma-shell -k AAAAAA dns.reload":         {RC: 0, Stdout: `{"id":"job1","finished":false,"error":false}`},
		"ma-shell -k AAAAAA job.status id job1": {RC: 0, Stdout: `{"id":"job1","finished":true,"error":false,"status":"DONE"}`},
	})
	res, err := moduleMemsetDNSReload(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "poll": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["stderr"] != nil {
		t.Fatalf("want no stderr, got %+v", res.Extra["stderr"])
	}
	meta, _ := res.Extra["memset_api"].(map[string]any)
	if meta["status"] != "DONE" {
		t.Fatalf("meta = %+v", res.Extra["memset_api"])
	}
	// command -v ma-shell, dns.reload, then exactly one job.status poll.
	if len(conn.Commands) != 3 {
		t.Fatalf("commands = %+v", conn.Commands)
	}
}

func TestModuleMemsetDNSReloadPollJobError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                   {RC: 0},
		"ma-shell -k AAAAAA dns.reload":         {RC: 0, Stdout: `{"id":"job1","finished":false,"error":false}`},
		"ma-shell -k AAAAAA job.status id job1": {RC: 0, Stdout: `{"id":"job1","finished":true,"error":true}`},
	})
	res, err := moduleMemsetDNSReload(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "poll": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed: res = %+v", res)
	}
	if res.Extra["stderr"] == nil {
		t.Fatalf("want stderr note about the job error, res = %+v", res)
	}
}

func TestModuleMemsetDNSReloadMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell": {RC: 1},
	})
	res, err := moduleMemsetDNSReload(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleMemsetDNSReloadAuthFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":           {RC: 0},
		"ma-shell -k BADKEY dns.reload": {RC: 2, Stderr: "Failed to connect to the server: <ProtocolError>"},
	})
	res, err := moduleMemsetDNSReload(context.Background(), conn, map[string]any{
		"api_key": "BADKEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleMemsetDNSReloadAPIFaultNonJSONOutput(t *testing.T) {
	// Exit 0, but ma-shell's own "<method>: <error>" text on stdout —
	// see memset_common.go's own doc comment on this verified quirk.
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":           {RC: 0},
		"ma-shell -k AAAAAA dns.reload": {RC: 0, Stdout: "dns.reload: <Fault 1: 'Invalid scope'>"},
	})
	res, err := moduleMemsetDNSReload(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
