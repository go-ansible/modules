package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaDnszoneCreate(t *testing.T) {
	showCmd := "ipa dnszone-show example.com --all --raw"
	addCmd := "ipa dnszone-add example.com --idnsallowdynupdate=TRUE"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaDnszone(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "dynamicupdate": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, commands = %v", res, fc.Commands)
	}
}

func TestModuleIpaDnszoneModify(t *testing.T) {
	showCmd := "ipa dnszone-show example.com --all --raw"
	modCmd := "ipa dnszone-mod example.com --idnsallowsyncptr=TRUE"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  idnsname: example.com\n  idnsallowsyncptr: FALSE\n  idnsallowdynupdate: FALSE\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaDnszone(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "allowsyncptr": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaDnszoneAlreadyUpToDate(t *testing.T) {
	showCmd := "ipa dnszone-show example.com --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  idnsname: example.com\n  idnsallowsyncptr: TRUE\n"},
	})
	res, err := moduleIpaDnszone(context.Background(), fc, map[string]any{
		"zone_name": "example.com", "allowsyncptr": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleIpaDnszoneDelete(t *testing.T) {
	showCmd := "ipa dnszone-show example.com --all --raw"
	delCmd := "ipa dnszone-del example.com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  idnsname: example.com\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaDnszone(context.Background(), fc, map[string]any{"zone_name": "example.com", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaDnszoneMissingZoneName(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaDnszone(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing zone_name")
	}
}
