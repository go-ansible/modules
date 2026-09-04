package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXenserverFacts(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe host-list params=uuid --minimal":    {RC: 0, Stdout: "host-uuid-1"},
		"xe host-param-list uuid=host-uuid-1":   {RC: 0, Stdout: "    software-version (MRW): product_version: 6.2.0; platform_version: 2.6.0\n"},
		"xe vlan-list params=uuid --minimal":    {RC: 0, Stdout: ""},
		"xe pif-list params=uuid --minimal":     {RC: 0, Stdout: ""},
		"xe network-list params=uuid --minimal": {RC: 0, Stdout: ""},
		"xe vm-list params=uuid --minimal":      {RC: 0, Stdout: ""},
		"xe sr-list params=uuid --minimal":      {RC: 0, Stdout: ""},
	})
	res, err := moduleXenserverFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Facts["xenserver_version"] != "6.2.0" {
		t.Fatalf("xenserver_version = %v", res.Facts["xenserver_version"])
	}
	if res.Facts["xenserver_codename"] != "clearwater" {
		t.Fatalf("xenserver_codename = %v", res.Facts["xenserver_codename"])
	}
	for _, absent := range []string{"xs_vlans", "xs_pifs", "xs_networks", "xs_vms", "xs_srs"} {
		if _, ok := res.Facts[absent]; ok {
			t.Errorf("want %s omitted when empty", absent)
		}
	}
}

func TestModuleXenserverFactsWithVMs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe host-list params=uuid --minimal":    {RC: 0, Stdout: ""},
		"xe vlan-list params=uuid --minimal":    {RC: 0, Stdout: ""},
		"xe pif-list params=uuid --minimal":     {RC: 0, Stdout: ""},
		"xe network-list params=uuid --minimal": {RC: 0, Stdout: ""},
		"xe vm-list params=uuid --minimal":      {RC: 0, Stdout: "vm-uuid-1"},
		"xe vm-param-list uuid=vm-uuid-1":       {RC: 0, Stdout: "    uuid ( RO): vm-uuid-1\n    name-label ( RW): myvm\n    power-state ( RO): running\n"},
		"xe sr-list params=uuid --minimal":      {RC: 0, Stdout: ""},
	})
	res, err := moduleXenserverFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	vms, ok := res.Facts["xs_vms"].(map[string]any)
	if !ok {
		t.Fatalf("xs_vms = %v (%T)", res.Facts["xs_vms"], res.Facts["xs_vms"])
	}
	vm, ok := vms["myvm"].(map[string]any)
	if !ok {
		t.Fatalf("xs_vms[myvm] = %v", vms["myvm"])
	}
	if vm["power-state"] != "running" {
		t.Fatalf("power-state = %v", vm["power-state"])
	}
	if vm["ref"] != "vm-uuid-1" {
		t.Fatalf("ref = %v", vm["ref"])
	}
}
