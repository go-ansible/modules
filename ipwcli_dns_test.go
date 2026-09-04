package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpwcliDnsCreateA(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ipwcli -user=admin -password=pw": {RC: 0, Stdout: "1 object(s) created.\n"},
	})
	// First call is the "list" probe, second the "create" — fakeConn
	// keys by exact command string, and both calls use the identical
	// ipwcli invocation (only stdin differs), so a single scripted
	// result answers both; assert on Stdins to distinguish them.
	res, err := moduleIpwcliDns(context.Background(), conn, map[string]any{
		"dnsname":   "example.com",
		"type":      "A",
		"container": "ZoneOne",
		"address":   "127.0.0.1",
		"username":  "admin",
		"password":  "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want Changed=true")
	}
	if len(conn.Stdins) != 2 {
		t.Fatalf("expected 2 ipwcli calls (list, create), got %d: %v", len(conn.Stdins), conn.Stdins)
	}
	if conn.Stdins[0] != "list arecord example.com 127.0.0.1 -where ttl=3600&&container=ZoneOne" {
		t.Fatalf("list stdin = %q", conn.Stdins[0])
	}
	if conn.Stdins[1] != "create arecord example.com 127.0.0.1 -set ttl=3600;container=ZoneOne" {
		t.Fatalf("create stdin = %q", conn.Stdins[1])
	}
	if res.Extra["record"] != "arecord example.com 127.0.0.1 -set ttl=3600;container=ZoneOne" {
		t.Fatalf("record = %+v", res.Extra["record"])
	}
}

func TestModuleIpwcliDnsCreateAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ipwcli -user=admin -password=pw": {RC: 0, Stdout: "ARecord example.com\n"},
	})
	res, err := moduleIpwcliDns(context.Background(), conn, map[string]any{
		"dnsname":   "example.com",
		"type":      "A",
		"container": "ZoneOne",
		"address":   "127.0.0.1",
		"username":  "admin",
		"password":  "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("expected only the list probe, got %v", conn.Commands)
	}
}

func TestModuleIpwcliDnsDeleteSRV(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ipwcli -user=admin -password=pw": {RC: 0, Stdout: "SRVRecord _sip._tcp.test.example.com\n1 object(s) were updated.\n"},
	})
	res, err := moduleIpwcliDns(context.Background(), conn, map[string]any{
		"dnsname":   "_sip._tcp.test.example.com",
		"type":      "SRV",
		"container": "ZoneOne",
		"ttl":       100,
		"state":     "absent",
		"target":    "example.com",
		"port":      5060,
		"username":  "admin",
		"password":  "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want Changed=true")
	}
}

func TestModuleIpwcliDnsCreateNAPTR(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleIpwcliDns(context.Background(), conn, map[string]any{
		"dnsname":     "test.example.com",
		"type":        "NAPTR",
		"preference":  10,
		"container":   "ZoneOne",
		"ttl":         100,
		"order":       10,
		"service":     "SIP+D2T",
		"replacement": "_sip._tcp.test.example.com.",
		"flags":       "S",
		"username":    "admin",
		"password":    "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	// fakeConn returns a zero Result (RC=0, empty stdout) for any
	// unscripted command, so this exercises record construction and
	// the "not found -> create -> unexpected output -> Fail" path.
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
	wantRecord := `naptrrecord test.example.com -set ttl=100;container=ZoneOne;order=10;preference=10;flags="S";service="SIP+D2T";replacement="_sip._tcp.test.example.com."`
	if len(conn.Stdins) < 1 || conn.Stdins[0] != `list naptrrecord test.example.com -where ttl=100&&container=ZoneOne&&order=10&&preference=10&&flags="S"&&service="SIP+D2T"&&replacement="_sip._tcp.test.example.com."` {
		t.Fatalf("list stdin = %+v, want record %q", conn.Stdins, wantRecord)
	}
}

func TestModuleIpwcliDnsInvalidLogin(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ipwcli -user=admin -password=wrong": {RC: 0, Stdout: "Invalid username or password\n"},
	})
	res, err := moduleIpwcliDns(context.Background(), conn, map[string]any{
		"dnsname":   "example.com",
		"type":      "A",
		"container": "ZoneOne",
		"address":   "127.0.0.1",
		"username":  "admin",
		"password":  "wrong",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModuleIpwcliDnsMissingAddress(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleIpwcliDns(context.Background(), conn, map[string]any{
		"dnsname":   "example.com",
		"type":      "A",
		"container": "ZoneOne",
		"username":  "admin",
		"password":  "pw",
	})
	if err == nil {
		t.Fatal("want error for missing address")
	}
}
