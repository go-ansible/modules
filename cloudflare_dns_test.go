package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleCloudflareDnsCreatesARecord(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v flarectl": {RC: 0},
		"CF_API_TOKEN=tok flarectl --json dns list --zone example.net --type A --name test.example.net --content 127.0.0.1":           {RC: 0, Stdout: "[]"},
		"CF_API_TOKEN=tok flarectl --json dns create --zone example.net --name test.example.net --type A --content 127.0.0.1 --ttl 1": {RC: 0},
	})
	res, err := moduleCloudflareDns(context.Background(), conn, map[string]any{
		"zone": "example.net", "record": "test", "type": "A", "value": "127.0.0.1", "api_token": "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleCloudflareDnsUnsupportedTypeFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v flarectl": {RC: 0},
	})
	res, err := moduleCloudflareDns(context.Background(), conn, map[string]any{
		"zone": "example.net", "record": "test", "type": "SRV", "value": "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleCloudflareDnsUpdatesCNAME(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v flarectl": {RC: 0},
		"flarectl --json dns list --zone example.net --type CNAME --name example.net":                                           {RC: 0, Stdout: `[{"id":"rec1","name":"example.net","type":"CNAME","content":"old.example.com","ttl":1,"proxied":false}]`},
		"flarectl --json dns update --zone example.net --id rec1 --name example.net --type CNAME --content example.com --ttl 1": {RC: 0},
	})
	res, err := moduleCloudflareDns(context.Background(), conn, map[string]any{
		"zone": "example.net", "type": "CNAME", "value": "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleCloudflareDnsAbsentRemoves(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v flarectl": {RC: 0},
		"flarectl --json dns list --zone example.net --type A --name test.example.net --content 127.0.0.1": {RC: 0, Stdout: `[{"id":"rec2","name":"test.example.net","type":"A","content":"127.0.0.1"}]`},
		"flarectl --json dns delete --zone example.net --id rec2":                                          {RC: 0},
	})
	res, err := moduleCloudflareDns(context.Background(), conn, map[string]any{
		"zone": "example.net", "record": "test", "type": "A", "value": "127.0.0.1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleCloudflareDnsMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v flarectl": {RC: 1},
	})
	res, err := moduleCloudflareDns(context.Background(), conn, map[string]any{
		"zone": "example.net", "record": "test", "type": "A", "value": "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleCloudflareDnsMissingZone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v flarectl": {RC: 0},
	})
	_, err := moduleCloudflareDns(context.Background(), conn, map[string]any{"type": "A", "value": "127.0.0.1"})
	if err == nil {
		t.Fatal("want error for missing zone")
	}
}
