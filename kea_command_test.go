package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeaCommandStatusGetUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kea-shell":                                  {RC: 0},
		"kea-shell --host 192.0.2.1 --service dhcp4 status-get": {RC: 0, Stdout: `{"result":0,"arguments":{"pid":123}}`},
	})
	res, err := moduleKeaCommand(context.Background(), conn, map[string]any{
		"command":      "status-get",
		"host":         "192.0.2.1",
		"service":      "dhcp4",
		"rv_unchanged": []any{0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want unchanged, not failed; res = %+v", res)
	}
	if len(conn.Stdins) != 2 || conn.Stdins[1] != "{}" {
		t.Fatalf("expected empty-object stdin, got %q (stdins=%v)", conn.Stdins[1], conn.Stdins)
	}
}

func TestModuleKeaCommandLeaseDelChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kea-shell":                  {RC: 0},
		"kea-shell --host 192.0.2.1 lease4-del": {RC: 0, Stdout: `{"result":0}`},
	})
	res, err := moduleKeaCommand(context.Background(), conn, map[string]any{
		"command":    "lease4-del",
		"host":       "192.0.2.1",
		"arguments":  map[string]any{"ip-address": "192.168.123.45"},
		"rv_changed": []any{0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed; res = %+v", res)
	}
	if conn.Stdins[1] != `{"ip-address":"192.168.123.45"}` {
		t.Fatalf("unexpected stdin: %q", conn.Stdins[1])
	}
}

func TestModuleKeaCommandErrorResult(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kea-shell":                 {RC: 0},
		"kea-shell --host 192.0.2.1 bogus-cmd": {RC: 0, Stdout: `{"result":2,"text":"unsupported command"}`},
	})
	res, err := moduleKeaCommand(context.Background(), conn, map[string]any{
		"command": "bogus-cmd",
		"host":    "192.0.2.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleKeaCommandShellFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kea-shell":                  {RC: 0},
		"kea-shell --host 192.0.2.1 status-get": {RC: 1, Stderr: "connection refused"},
	})
	res, err := moduleKeaCommand(context.Background(), conn, map[string]any{
		"command": "status-get",
		"host":    "192.0.2.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !res.Changed {
		t.Fatalf("want failed AND changed=true (err-to-the-safe-side), res = %+v", res)
	}
}

func TestModuleKeaCommandMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kea-shell": {RC: 1},
	})
	res, err := moduleKeaCommand(context.Background(), conn, map[string]any{
		"command": "status-get",
		"host":    "192.0.2.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleKeaCommandMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleKeaCommand(context.Background(), conn, map[string]any{"command": "status-get"})
	if err == nil {
		t.Fatal("want error for missing required host")
	}
}
