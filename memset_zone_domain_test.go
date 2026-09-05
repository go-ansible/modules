package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMemsetZoneDomainCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                                                     {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":                                        {RC: 0, Stdout: `[{"id":"zone1","nickname":"testzone"}]`},
		"ma-shell -k AAAAAA dns.zone_domain_list":                                 {RC: 0, Stdout: `[]`},
		"ma-shell -k AAAAAA dns.zone_domain_create domain test.com zone_id zone1": {RC: 0, Stdout: `{"id":"dom1","domain":"test.com"}`},
		"ma-shell -k AAAAAA dns.zone_domain_info domain test.com":                 {RC: 0, Stdout: `{"id":"dom1","domain":"test.com"}`},
	})
	res, err := moduleMemsetZoneDomain(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "domain": "test.com", "zone": "testzone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneDomainPresentAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                                     {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":                        {RC: 0, Stdout: `[{"id":"zone1","nickname":"testzone"}]`},
		"ma-shell -k AAAAAA dns.zone_domain_list":                 {RC: 0, Stdout: `[{"id":"dom1","domain":"test.com"}]`},
		"ma-shell -k AAAAAA dns.zone_domain_info domain test.com": {RC: 0, Stdout: `{"id":"dom1","domain":"test.com"}`},
	})
	res, err := moduleMemsetZoneDomain(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "domain": "test.com", "zone": "testzone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneDomainZoneNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":              {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleMemsetZoneDomain(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "domain": "test.com", "zone": "testzone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (zone does not exist), res = %+v", res)
	}
}

func TestModuleMemsetZoneDomainAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                                       {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":                          {RC: 0, Stdout: `[{"id":"zone1","nickname":"testzone"}]`},
		"ma-shell -k AAAAAA dns.zone_domain_list":                   {RC: 0, Stdout: `[{"id":"dom1","domain":"test.com"}]`},
		"ma-shell -k AAAAAA dns.zone_domain_delete domain test.com": {RC: 0, Stdout: `{"id":"dom1"}`},
	})
	res, err := moduleMemsetZoneDomain(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "domain": "test.com", "zone": "testzone", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneDomainAbsentNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                     {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":        {RC: 0, Stdout: `[{"id":"zone1","nickname":"testzone"}]`},
		"ma-shell -k AAAAAA dns.zone_domain_list": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleMemsetZoneDomain(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "domain": "test.com", "zone": "testzone", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneDomainTooLong(t *testing.T) {
	conn := newFakeConn(nil)
	long := ""
	for i := 0; i < 251; i++ {
		long += "a"
	}
	if _, err := moduleMemsetZoneDomain(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "domain": long, "zone": "testzone",
	}); err == nil {
		t.Fatal("want error for domain too long")
	}
}

func TestModuleMemsetZoneDomainMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell": {RC: 1},
	})
	res, err := moduleMemsetZoneDomain(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "domain": "test.com", "zone": "testzone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
