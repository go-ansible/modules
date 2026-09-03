package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaDnsrecordCreate(t *testing.T) {
	showCmd := "ipa dnsrecord-show example.com vm-001 --all --raw"
	addCmd := "ipa dnsrecord-add example.com vm-001 --aaaarecord=::1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "vm-001", "record_type": "AAAA", "record_value": "::1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaDnsrecordExclusiveReplace(t *testing.T) {
	showCmd := "ipa dnsrecord-show example.com host02 --all --raw"
	modCmd := "ipa dnsrecord-mod example.com host02 --aaaarecord=::1 --aaaarecord=fe80::1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  idnsname: host02\n  aaaarecord: ::2\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "host02", "record_type": "AAAA",
		"record_values": []any{"::1", "fe80::1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaDnsrecordNonExclusiveAddsOnly(t *testing.T) {
	showCmd := "ipa dnsrecord-show example.com _etcd-server-ssl._tcp.cloud.example.com. --all --raw"
	addCmd := "ipa dnsrecord-add example.com _etcd-server-ssl._tcp.cloud.example.com. --srvrecord=0 10 2380 etcd-0.cloud.example.com."
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd: {RC: 0, Stdout: "  idnsname: _etcd-server-ssl._tcp.cloud.example.com.\n" +
			"  srvrecord: 0 10 2379 etcd-1.cloud.example.com.\n"},
		addCmd: {RC: 0},
	})
	res, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "_etcd-server-ssl._tcp.cloud.example.com.",
		"record_type": "SRV", "record_value": "0 10 2380 etcd-0.cloud.example.com.", "exclusive": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaDnsrecordAlreadySet(t *testing.T) {
	showCmd := "ipa dnsrecord-show example.com vm-001 --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  idnsname: vm-001\n  aaaarecord: ::1\n"},
	})
	res, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "vm-001", "record_type": "AAAA", "record_value": "::1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleIpaDnsrecordAbsentExclusiveClearsAll(t *testing.T) {
	showCmd := "ipa dnsrecord-show example.com host01 --all --raw"
	clearCmd := "ipa dnsrecord-mod example.com host01 --aaaarecord="
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  idnsname: host01\n  aaaarecord: ::1\n"},
		clearCmd:         {RC: 0},
	})
	res, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "host01", "record_type": "AAAA",
		"record_value": "::1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaDnsrecordAbsentNonExclusiveRemovesListed(t *testing.T) {
	showCmd := "ipa dnsrecord-show example.com host01 --all --raw"
	delCmd := "ipa dnsrecord-del example.com host01 --aaaarecord=::1"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  idnsname: host01\n  aaaarecord: ::1\n  aaaarecord: ::2\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "host01", "record_type": "AAAA",
		"record_value": "::1", "state": "absent", "exclusive": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaDnsrecordMutuallyExclusiveValues(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "x", "record_value": "1", "record_values": []any{"2"},
	}); err == nil {
		t.Fatal("want error for both record_value and record_values given")
	}
}

func TestModuleIpaDnsrecordMissingValue(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "x",
	}); err == nil {
		t.Fatal("want error when neither record_value nor record_values given")
	}
}

func TestModuleIpaDnsrecordInvalidType(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "record_name": "x", "record_value": "1", "record_type": "BOGUS",
	}); err == nil {
		t.Fatal("want error for invalid record_type")
	}
}

func TestModuleIpaDnsrecordMissingZoneOrName(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{"record_name": "x", "record_value": "1"}); err == nil {
		t.Fatal("want error for missing zone_name")
	}
	if _, err := moduleIpaDnsrecord(context.Background(), fc, map[string]any{"zone_name": "example.com", "record_value": "1"}); err == nil {
		t.Fatal("want error for missing record_name")
	}
}
