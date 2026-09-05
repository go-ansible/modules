package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDnsimpleInfoAllDomains(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple":          {RC: 0},
		"dnsimple domains list --json": {RC: 0, Stdout: `{"data":[{"id":1,"name":"example.com"}]}`},
	})
	res, err := moduleDnsimpleInfo(context.Background(), conn, map[string]any{"account_id": "1", "api_key": "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	if res.Extra["dnsimple_domain_info"] == nil {
		t.Fatal("want dnsimple_domain_info in result")
	}
}

func TestModuleDnsimpleInfoAllRecordsForDomain(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple":                      {RC: 0},
		"dnsimple records list example.com --json": {RC: 0, Stdout: `{"data":[{"id":1,"name":"catheadbiscuit"}]}`},
	})
	res, err := moduleDnsimpleInfo(context.Background(), conn, map[string]any{
		"account_id": "1", "api_key": "tok", "name": "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["dnsimple_records_info"] == nil {
		t.Fatal("want dnsimple_records_info in result")
	}
}

func TestModuleDnsimpleInfoSingleRecord(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple": {RC: 0},
		"dnsimple records list example.com --name catheadbiscuit --json": {RC: 0, Stdout: `{"data":[{"id":1,"name":"catheadbiscuit"}]}`},
	})
	res, err := moduleDnsimpleInfo(context.Background(), conn, map[string]any{
		"account_id": "1", "api_key": "tok", "name": "example.com", "record": "catheadbiscuit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["dnsimple_record_info"] == nil {
		t.Fatal("want dnsimple_record_info in result")
	}
}
