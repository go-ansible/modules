package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMemsetZoneCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                                              {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":                                 {RC: 0, Stdout: `[]`},
		"ma-shell -k AAAAAA dns.zone_create nickname test '(int)' ttl 300": {RC: 0, Stdout: `{"id":"zone1","nickname":"test","ttl":300}`},
		"ma-shell -k AAAAAA dns.zone_info id zone1":                        {RC: 0, Stdout: `{"id":"zone1","nickname":"test","ttl":300,"domains":[],"records":[]}`},
	})
	res, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "present", "ttl": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZonePresentAlreadyCorrect(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                       {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":          {RC: 0, Stdout: `[{"id":"zone1","nickname":"test","ttl":300,"domains":[],"records":[]}]`},
		"ma-shell -k AAAAAA dns.zone_info id zone1": {RC: 0, Stdout: `{"id":"zone1","nickname":"test","ttl":300,"domains":[],"records":[]}`},
	})
	res, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "present", "ttl": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneUpdateTTL(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                                         {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":                            {RC: 0, Stdout: `[{"id":"zone1","nickname":"test","ttl":300,"domains":[],"records":[]}]`},
		"ma-shell -k AAAAAA dns.zone_update id zone1 '(int)' ttl 600": {RC: 0, Stdout: `{"id":"zone1"}`},
		"ma-shell -k AAAAAA dns.zone_info id zone1":                   {RC: 0, Stdout: `{"id":"zone1","nickname":"test","ttl":600,"domains":[],"records":[]}`},
	})
	res, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "present", "ttl": 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneAbsentNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":              {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                         {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":            {RC: 0, Stdout: `[{"id":"zone1","nickname":"test","ttl":0,"domains":[],"records":[]}]`},
		"ma-shell -k AAAAAA dns.zone_delete id zone1": {RC: 0, Stdout: `{"id":"zone1"}`},
	})
	res, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneAbsentRefusesWithoutForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":              {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list": {RC: 0, Stdout: `[{"id":"zone1","nickname":"test","ttl":0,"domains":["a.com"],"records":[]}]`},
	})
	res, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (contains domains, no force), res = %+v", res)
	}
}

func TestModuleMemsetZoneAbsentAmbiguous(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":              {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list": {RC: 0, Stdout: `[{"id":"zone1","nickname":"test","domains":[],"records":[]},{"id":"zone2","nickname":"test","domains":[],"records":[]}]`},
	})
	res, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (ambiguous), res = %+v", res)
	}
}

func TestModuleMemsetZoneInvalidTTL(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "present", "ttl": 42,
	}); err == nil {
		t.Fatal("want error for invalid ttl")
	}
}

func TestModuleMemsetZoneMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell": {RC: 1},
	})
	res, err := moduleMemsetZone(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "name": "test", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
