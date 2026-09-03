package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNmcliCreateEthernet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nmcli -g connection.id connection show eth1": {RC: 1},
		"nmcli connection add type ethernet con-name eth1 ifname eth1 connection.autoconnect yes " +
			"ipv4.addresses 192.0.2.24/24 ipv4.gateway 192.0.2.1 ipv4.method manual": {RC: 0},
	})
	res, err := moduleNmcli(context.Background(), conn, map[string]any{
		"conn_name": "eth1", "type": "ethernet", "state": "present", "ifname": "eth1",
		"ip4": []any{"192.0.2.24/24"}, "gw4": "192.0.2.1", "method4": "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleNmcliAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nmcli -g connection.id connection show eth1": {RC: 1},
	})
	res, err := moduleNmcli(context.Background(), conn, map[string]any{
		"conn_name": "eth1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already absent")
	}
}

func TestModuleNmcliDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nmcli -g connection.id connection show eth1": {RC: 0, Stdout: "eth1\n"},
		"nmcli connection delete eth1":                {RC: 0},
	})
	res, err := moduleNmcli(context.Background(), conn, map[string]any{
		"conn_name": "eth1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleNmcliModifyOnlyChangedFields(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nmcli -g connection.id connection show eth1":          {RC: 0, Stdout: "eth1\n"},
		"nmcli -g connection.autoconnect connection show eth1": {RC: 0, Stdout: "yes\n"},
		"nmcli -g ipv4.addresses connection show eth1":         {RC: 0, Stdout: "192.0.2.1/24\n"},
		"nmcli -g ipv4.gateway connection show eth1":           {RC: 0, Stdout: "192.0.2.254\n"},
		"nmcli connection modify eth1 ipv4.gateway 192.0.2.1":  {RC: 0},
	})
	res, err := moduleNmcli(context.Background(), conn, map[string]any{
		"conn_name": "eth1", "type": "ethernet", "state": "present",
		"ip4": []any{"192.0.2.1/24"}, "gw4": "192.0.2.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "nmcli connection modify eth1 ipv4.gateway 192.0.2.1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleNmcliModifyIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nmcli -g connection.id connection show eth1":          {RC: 0, Stdout: "eth1\n"},
		"nmcli -g connection.autoconnect connection show eth1": {RC: 0, Stdout: "yes\n"},
		"nmcli -g ipv4.addresses connection show eth1":         {RC: 0, Stdout: "192.0.2.1/24\n"},
	})
	res, err := moduleNmcli(context.Background(), conn, map[string]any{
		"conn_name": "eth1", "type": "ethernet", "state": "present",
		"ip4": []any{"192.0.2.1/24"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}

func TestModuleNmcliBondMode(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nmcli -g connection.id connection show bond0":                                                             {RC: 1},
		"nmcli connection add type bond con-name bond0 connection.autoconnect yes bond.options mode=active-backup": {RC: 0},
	})
	res, err := moduleNmcli(context.Background(), conn, map[string]any{
		"conn_name": "bond0", "type": "bond", "state": "present", "mode": "active-backup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleNmcliUnsupportedType(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nmcli -g connection.id connection show wifi0": {RC: 1},
	})
	res, err := moduleNmcli(context.Background(), conn, map[string]any{
		"conn_name": "wifi0", "type": "wifi", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed for unsupported type")
	}
}

func TestModuleNmcliInvalidMethod4(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nmcli -g connection.id connection show eth1": {RC: 1},
	})
	_, err := moduleNmcli(context.Background(), conn, map[string]any{
		"conn_name": "eth1", "type": "ethernet", "state": "present", "method4": "bogus",
	})
	if err == nil {
		t.Fatal("want error for invalid method4")
	}
}

func TestModuleNmcliMissingConnName(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleNmcli(context.Background(), conn, map[string]any{"state": "present", "type": "ethernet"})
	if err == nil {
		t.Fatal("want error for missing conn_name")
	}
}

func TestModuleNmcliMissingState(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleNmcli(context.Background(), conn, map[string]any{"conn_name": "eth1", "type": "ethernet"})
	if err == nil {
		t.Fatal("want error for missing state")
	}
}
