package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLLDPFactsBasic(t *testing.T) {
	out := "lldp.eth2.chassis.name=switch1.example.com\n" +
		"lldp.eth2.chassis.mgmt-ip=10.0.0.1\n" +
		"lldp.eth2.port.ifname=Gi0/24\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lldpctl -f keyvalue": {RC: 0, Stdout: out},
	})
	res, err := moduleLLDPFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	lldp := res.Extra["lldp"].(map[string]any)
	eth2 := lldp["eth2"].(map[string]any)
	chassis := eth2["chassis"].(map[string]any)
	if chassis["name"] != "switch1.example.com" {
		t.Fatalf("chassis = %+v", chassis)
	}
	port := eth2["port"].(map[string]any)
	if port["ifname"] != "Gi0/24" {
		t.Fatalf("port = %+v", port)
	}
}

func TestModuleLLDPFactsContinuationLine(t *testing.T) {
	out := "lldp.eth0.lldp-med.inventory.software=1.0\n" +
		"continues here\n" +
		"lldp.eth0.port.ifname=eth0\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lldpctl -f keyvalue": {RC: 0, Stdout: out},
	})
	res, err := moduleLLDPFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	lldp := res.Extra["lldp"].(map[string]any)
	eth0 := lldp["eth0"].(map[string]any)
	inv := eth0["lldp-med"].(map[string]any)["inventory"].(map[string]any)
	if inv["software"] != "1.0\ncontinues here" {
		t.Fatalf("software = %q", inv["software"])
	}
}

func TestModuleLLDPFactsMultivalues(t *testing.T) {
	out := "lldp.eth0.vlan.name=vlan1\n" +
		"lldp.eth0.vlan.name=vlan2\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lldpctl -f keyvalue": {RC: 0, Stdout: out},
	})
	res, err := moduleLLDPFacts(context.Background(), conn, map[string]any{"multivalues": true})
	if err != nil {
		t.Fatal(err)
	}
	lldp := res.Extra["lldp"].(map[string]any)
	vlan := lldp["eth0"].(map[string]any)["vlan"].(map[string]any)
	names, ok := vlan["name"].([]string)
	if !ok || len(names) != 2 || names[0] != "vlan1" || names[1] != "vlan2" {
		t.Fatalf("name = %#v", vlan["name"])
	}
}

func TestModuleLLDPFactsWithoutMultivaluesKeepsLast(t *testing.T) {
	out := "lldp.eth0.vlan.name=vlan1\n" +
		"lldp.eth0.vlan.name=vlan2\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lldpctl -f keyvalue": {RC: 0, Stdout: out},
	})
	res, err := moduleLLDPFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	lldp := res.Extra["lldp"].(map[string]any)
	vlan := lldp["eth0"].(map[string]any)["vlan"].(map[string]any)
	if vlan["name"] != "vlan2" {
		t.Fatalf("name = %v", vlan["name"])
	}
}

func TestModuleLLDPFactsEmptyOutputFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lldpctl -f keyvalue": {RC: 1, Stdout: ""},
	})
	res, err := moduleLLDPFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for empty lldpctl output")
	}
}
