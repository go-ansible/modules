package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleInfinityAddNetwork(t *testing.T) {
	conn := newFakeConn(nil)
	// fakeConn returns a zero Result (RC 0, empty body) for any
	// unmatched command, which parseCurlStatus would reject (no
	// HTTPSTATUS marker) — so build the exact expected command and
	// script a matching response.
	body := `{"network_address":"192.168.310.0","network_family":"4","network_location":-1,"network_name":"n1","network_size":"/26","network_type":"lan"}`
	cmd := "curl -s -k -K - -w '\nHTTPSTATUS:%{http_code}' -X POST -H 'Content-Type: application/json' -d " +
		shellQuote(body) + " " + shellQuote("https://80.75.107.12/rest/v1/networks")
	conn.on = map[string]remoteexec.Result{
		"command -v curl": {RC: 0},
		cmd:               {RC: 0, Stdout: `{"network_id":1234}` + "\nHTTPSTATUS:200"},
	}
	res, err := moduleInfinity(context.Background(), conn, map[string]any{
		"server_ip":       "80.75.107.12",
		"username":        "u",
		"password":        "p",
		"action":          "add_network",
		"network_name":    "n1",
		"network_address": "192.168.310.0",
		"network_size":    "/26",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed; res = %+v", res)
	}
	if res.Extra["meta"] != `{"network_id":1234}` {
		t.Fatalf("unexpected meta: %v", res.Extra["meta"])
	}
	// Verify the credential pair never appears in the recorded command
	// string itself (only via the piped stdin config, checked below).
	for _, c := range conn.Commands {
		if strings.Contains(c, "u:p") {
			t.Fatalf("credentials leaked onto argv: %s", c)
		}
	}
	if len(conn.Stdins) < 2 || conn.Stdins[1] != `user = "u:p"`+"\n" {
		t.Fatalf("expected credentials via stdin config, got %q", conn.Stdins)
	}
}

func TestModuleInfinityMissingActionArgIsOkNotFailed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v curl": {RC: 0},
	})
	res, err := moduleInfinity(context.Background(), conn, map[string]any{
		"server_ip": "80.75.107.12",
		"username":  "u",
		"password":  "p",
		"action":    "reserve_next_available_ip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("real infinity.py never fails, only changed=false+msg; res = %+v", res)
	}
}

func TestModuleInfinityHTTPErrorIsOkNotFailed(t *testing.T) {
	conn := newFakeConn(nil)
	cmd := "curl -s -k -K - -w '\nHTTPSTATUS:%{http_code}' -X POST -H 'Content-Type: application/json' " +
		shellQuote("https://1.2.3.4/rest/v1/networks/10/reserve_ip")
	conn.on = map[string]remoteexec.Result{
		"command -v curl": {RC: 0},
		cmd:               {RC: 0, Stdout: "\nHTTPSTATUS:401"},
	}
	res, err := moduleInfinity(context.Background(), conn, map[string]any{
		"server_ip":  "1.2.3.4",
		"username":   "u",
		"password":   "p",
		"action":     "reserve_next_available_ip",
		"network_id": "10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want changed=false, failed=false (real module never fails); res = %+v", res)
	}
}

func TestModuleInfinityMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v curl": {RC: 1},
	})
	res, err := moduleInfinity(context.Background(), conn, map[string]any{
		"server_ip": "1.2.3.4", "username": "u", "password": "p", "action": "get_network", "network_id": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (infra: missing curl), res = %+v", res)
	}
}

func TestModuleInfinityMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleInfinity(context.Background(), conn, map[string]any{"server_ip": "1.2.3.4"})
	if err == nil {
		t.Fatal("want error for missing required args")
	}
}

func TestModuleInfinityInvalidAction(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v curl": {RC: 0},
	})
	_, err := moduleInfinity(context.Background(), conn, map[string]any{
		"server_ip": "1.2.3.4", "username": "u", "password": "p", "action": "bogus",
	})
	if err == nil {
		t.Fatal("want error for invalid action")
	}
}
