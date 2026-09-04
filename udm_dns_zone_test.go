package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUdmDnsZoneMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUdmDnsZone(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing zone/type")
	}
}

func TestModuleUdmDnsZoneRequiresNameserverAndInterfaces(t *testing.T) {
	findCmd := "udm dns/forward_zone list --filter zone=example.com"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: ""},
	})
	if _, err := moduleUdmDnsZone(context.Background(), conn, map[string]any{
		"zone": "example.com", "type": "forward_zone",
	}); err == nil {
		t.Fatal("want error for missing nameserver/interfaces")
	}
}

func TestModuleUdmDnsZoneCreate(t *testing.T) {
	findCmd := "udm dns/forward_zone list --filter zone=example.com"
	createCmd := "udm dns/forward_zone create --position cn=dns,dc=example,dc=com" +
		" --set a=192.0.2.1 --set contact=root@example.com. --set expire=604800" +
		" --set nameserver=ns.example.com --set refresh=3600 --set retry=1800 --set ttl=600 --set zone=example.com"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: ""},
		createCmd:            {RC: 0},
	})
	res, err := moduleUdmDnsZone(context.Background(), conn, map[string]any{
		"zone": "example.com", "type": "forward_zone",
		"nameserver": []any{"ns.example.com"},
		"interfaces": []any{"192.0.2.1"},
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

func TestModuleUdmDnsZoneAbsentAlready(t *testing.T) {
	findCmd := "udm dns/forward_zone list --filter zone=example.com"
	conn := newFakeConn(map[string]remoteexec.Result{
		"ucr get ldap/base": {RC: 0, Stdout: "dc=example,dc=com\n"},
		findCmd:              {RC: 0, Stdout: ""},
	})
	res, err := moduleUdmDnsZone(context.Background(), conn, map[string]any{
		"zone": "example.com", "type": "forward_zone", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}
