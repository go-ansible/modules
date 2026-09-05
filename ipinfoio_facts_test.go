package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpinfoioFacts(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ipinfo":  {RC: 0},
		"ipinfo myip --json": {RC: 0, Stdout: `{"ip":"8.8.8.8","hostname":"dns.google","city":"Mountain View","region":"California","country":"US","loc":"37.3860,-122.0838","org":"AS15169 Google LLC","postal":"94035"}`},
	})
	res, err := moduleIpinfoioFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	facts, ok := res.Extra["ansible_facts"].(map[string]any)
	if !ok {
		t.Fatalf("ansible_facts = %+v", res.Extra["ansible_facts"])
	}
	if facts["ip"] != "8.8.8.8" {
		t.Fatalf("ip = %v", facts["ip"])
	}
}

func TestModuleIpinfoioFactsMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ipinfo": {RC: 1},
	})
	res, err := moduleIpinfoioFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleIpinfoioFactsNonZeroExit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ipinfo":  {RC: 0},
		"ipinfo myip --json": {RC: 1, Stderr: "network error"},
	})
	res, err := moduleIpinfoioFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
