package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMemsetZoneRecordCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                     {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":        {RC: 0, Stdout: `[{"id":"zoneA","nickname":"domain.com"}]`},
		"ma-shell -k AAAAAA dns.zone_record_list": {RC: 0, Stdout: `[]`},
		"ma-shell -k AAAAAA dns.zone_record_create zone_id zoneA type A record www address 1.2.3.4 '(int)' ttl 300 '(int)' priority 0": {
			RC: 0, Stdout: `{"id":"rec1","zone_id":"zoneA","type":"A","record":"www","address":"1.2.3.4","ttl":300,"priority":0}`,
		},
	})
	res, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4", "ttl": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneRecordAlreadyMatches(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                     {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":        {RC: 0, Stdout: `[{"id":"zoneA","nickname":"domain.com"}]`},
		"ma-shell -k AAAAAA dns.zone_record_list": {RC: 0, Stdout: `[{"id":"rec1","zone_id":"zoneA","type":"A","record":"www","address":"1.2.3.4","ttl":300,"priority":0,"relative":false}]`},
	})
	res, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4", "ttl": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneRecordUpdatesOnMismatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                     {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":        {RC: 0, Stdout: `[{"id":"zoneA","nickname":"domain.com"}]`},
		"ma-shell -k AAAAAA dns.zone_record_list": {RC: 0, Stdout: `[{"id":"rec1","zone_id":"zoneA","type":"A","record":"www","address":"9.9.9.9","ttl":300,"priority":0,"relative":false}]`},
		"ma-shell -k AAAAAA dns.zone_record_update zone_id zoneA type A record www address 1.2.3.4 '(int)' ttl 300 '(int)' priority 0 id rec1": {
			RC: 0, Stdout: `{"id":"rec1","address":"1.2.3.4"}`,
		},
	})
	res, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4", "ttl": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneRecordAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                               {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":                  {RC: 0, Stdout: `[{"id":"zoneA","nickname":"domain.com"}]`},
		"ma-shell -k AAAAAA dns.zone_record_list":           {RC: 0, Stdout: `[{"id":"rec1","zone_id":"zoneA","type":"A","record":"www","address":"1.2.3.4","ttl":300,"priority":0}]`},
		"ma-shell -k AAAAAA dns.zone_record_delete id rec1": {RC: 0, Stdout: `{"id":"rec1"}`},
	})
	res, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneRecordAbsentNoMatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                     {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":        {RC: 0, Stdout: `[{"id":"zoneA","nickname":"domain.com"}]`},
		"ma-shell -k AAAAAA dns.zone_record_list": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneRecordRelativeTrueSent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":                     {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list":        {RC: 0, Stdout: `[{"id":"zoneA","nickname":"domain.com"}]`},
		"ma-shell -k AAAAAA dns.zone_record_list": {RC: 0, Stdout: `[]`},
		"ma-shell -k AAAAAA dns.zone_record_create zone_id zoneA type CNAME record www address target.example.com '(int)' ttl 0 '(int)' priority 0 '(boolean)' relative true": {
			RC: 0, Stdout: `{"id":"rec1"}`,
		},
	})
	res, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "CNAME", "record": "www",
		"address": "target.example.com", "relative": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMemsetZoneRecordRelativeInvalidForType(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4", "relative": true,
	}); err == nil {
		t.Fatal("want error: relative only valid for CNAME/MX/NS/SRV")
	}
}

func TestModuleMemsetZoneRecordPriorityOutOfRange(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4", "priority": 1000,
	}); err == nil {
		t.Fatal("want error: priority out of range")
	}
}

func TestModuleMemsetZoneRecordZoneNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell":              {RC: 0},
		"ma-shell -k AAAAAA dns.zone_list": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed (zone does not exist), res = %+v", res)
	}
}

func TestModuleMemsetZoneRecordMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ma-shell": {RC: 1},
	})
	res, err := moduleMemsetZoneRecord(context.Background(), conn, map[string]any{
		"api_key": "AAAAAA", "zone": "domain.com", "type": "A", "record": "www",
		"address": "1.2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
