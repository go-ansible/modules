package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const testZoneListAll = `public (active)
  target: default
  icmp-block-inversion: no
  interfaces: eth0
  sources:
  services: dhcpv6-client ssh
  ports:
  protocols:
  forward: no
  masquerade: no
  forward-ports:
  source-ports:
  icmp-blocks:
  rich rules:
`

func TestModuleFirewalldInfoAllZones(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --get-default-zone":       {RC: 0, Stdout: "public"},
		"firewall-cmd --version":                {RC: 0, Stdout: "0.8.2"},
		"firewall-cmd --get-zones":              {RC: 0, Stdout: "public work"},
		"firewall-cmd --zone=public --list-all": {RC: 0, Stdout: testZoneListAll},
		"firewall-cmd --zone=work --list-all":   {RC: 0, Stdout: testZoneListAll},
	})
	res, err := moduleFirewalldInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: gather-only")
	}
	collected := res.Extra["collected_zones"].([]string)
	if len(collected) != 2 {
		t.Fatalf("collected_zones = %v", collected)
	}
	info := res.Extra["firewalld_info"].(map[string]any)
	if info["default_zone"] != "public" {
		t.Fatalf("default_zone = %v", info["default_zone"])
	}
	zones := info["zones"].(map[string]any)
	pub := zones["public"].(map[string]any)
	if pub["target"] != "default" {
		t.Fatalf("target = %v", pub["target"])
	}
	if pub["masquerade"] != false {
		t.Fatalf("masquerade = %v", pub["masquerade"])
	}
	services := pub["services"].([]string)
	if len(services) != 2 {
		t.Fatalf("services = %v", services)
	}
}

func TestModuleFirewalldInfoRequestedZonesWithUndefined(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --get-default-zone":       {RC: 0, Stdout: "public"},
		"firewall-cmd --version":                {RC: 0, Stdout: "0.8.2"},
		"firewall-cmd --get-zones":              {RC: 0, Stdout: "public work"},
		"firewall-cmd --zone=public --list-all": {RC: 0, Stdout: testZoneListAll},
	})
	res, err := moduleFirewalldInfo(context.Background(), conn, map[string]any{
		"zones": []any{"public", "bogus"},
	})
	if err != nil {
		t.Fatal(err)
	}
	undefined := res.Extra["undefined_zones"].([]string)
	if len(undefined) != 1 || undefined[0] != "bogus" {
		t.Fatalf("undefined_zones = %v", undefined)
	}
}

func TestModuleFirewalldInfoActiveZones(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --get-default-zone":       {RC: 0, Stdout: "public"},
		"firewall-cmd --version":                {RC: 0, Stdout: "0.8.2"},
		"firewall-cmd --get-zones":              {RC: 0, Stdout: "public work"},
		"firewall-cmd --get-active-zones":       {RC: 0, Stdout: "public\n  interfaces: eth0\n"},
		"firewall-cmd --zone=public --list-all": {RC: 0, Stdout: testZoneListAll},
	})
	res, err := moduleFirewalldInfo(context.Background(), conn, map[string]any{"active_zones": true})
	if err != nil {
		t.Fatal(err)
	}
	collected := res.Extra["collected_zones"].([]string)
	if len(collected) != 1 || collected[0] != "public" {
		t.Fatalf("collected_zones = %v", collected)
	}
}
