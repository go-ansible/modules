package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUdmDnsRecordMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUdmDnsRecord(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name/zone/type")
	}
}

func TestModuleUdmDnsRecordInvalidType(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleUdmDnsRecord(context.Background(), conn, map[string]any{
		"name": "www", "zone": "example.com", "type": "bogus",
	})
	if err == nil {
		t.Fatal("want error for invalid type")
	}
}

func TestModuleUdmDnsRecordCreate(t *testing.T) {
	zoneDN := "zoneName=example.com,cn=dns,dc=example,dc=com"
	findCmd := "udm dns/host_record list --filter name=www --superordinate " + zoneDN
	createCmd := "udm dns/host_record create --superordinate " + zoneDN + " --set a=192.0.2.1 --set name=www"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: ""},
		createCmd:            {RC: 0},
	})
	res, err := moduleUdmDnsRecord(context.Background(), conn, map[string]any{
		"name": "www", "zone": "example.com", "type": "host_record",
		"data": map[string]any{"a": "192.0.2.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleUdmDnsRecordAbsentAlready(t *testing.T) {
	zoneDN := "zoneName=example.com,cn=dns,dc=example,dc=com"
	findCmd := "udm dns/host_record list --filter name=www --superordinate " + zoneDN
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: ""},
	})
	res, err := moduleUdmDnsRecord(context.Background(), conn, map[string]any{
		"name": "www", "zone": "example.com", "type": "host_record", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleUdmDnsRecordPTR(t *testing.T) {
	// 192.1.1.5's reverse pointer is "5.1.1.192.in-addr.arpa"; stripping
	// the zone "1.1.192.in-addr.arpa" suffix leaves workname "5".
	zoneDN := "zoneName=1.1.192.in-addr.arpa,cn=dns,dc=example,dc=com"
	findCmd := "udm dns/ptr_record list --filter name=5 --superordinate " + zoneDN
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: ""},
	})
	res, err := moduleUdmDnsRecord(context.Background(), conn, map[string]any{
		"name": "192.1.1.5", "zone": "1.1.192.in-addr.arpa", "type": "ptr_record",
		"data": map[string]any{"ptr_record": "www.example.com."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleUdmDnsRecordPTRRejectsNonArpaZone(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleUdmDnsRecord(context.Background(), conn, map[string]any{
		"name": "192.168.1.1", "zone": "example.com", "type": "ptr_record",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for non-arpa zone with ptr_record")
	}
}
