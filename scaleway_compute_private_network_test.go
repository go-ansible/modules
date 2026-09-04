package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const scwPNList = "scw instance private-nic list server-id=srv-1 zone=fr-par-1 -o json"

func TestModuleScalewayComputePrivateNetworkAttach(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwPNList: {RC: 0, Stdout: `[]`},
		"scw instance private-nic create server-id=srv-1 private-network-id=pn-1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"nic-1","server_id":"srv-1","private_network_id":"pn-1"}`,
		},
	})
	res, err := moduleScalewayComputePrivateNetwork(context.Background(), conn, map[string]any{
		"compute_id": "srv-1", "private_network_id": "pn-1", "project": "proj", "region": "par1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputePrivateNetworkAlreadyAttached(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwPNList: {RC: 0, Stdout: `[{"id":"nic-1","server_id":"srv-1","private_network_id":"pn-1"}]`},
	})
	res, err := moduleScalewayComputePrivateNetwork(context.Background(), conn, map[string]any{
		"compute_id": "srv-1", "private_network_id": "pn-1", "project": "proj", "region": "par1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputePrivateNetworkDetach(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwPNList: {RC: 0, Stdout: `[{"id":"nic-1","server_id":"srv-1","private_network_id":"pn-1"}]`},
		"scw instance private-nic delete server-id=srv-1 private-nic-id=nic-1 zone=fr-par-1": {RC: 0},
	})
	res, err := moduleScalewayComputePrivateNetwork(context.Background(), conn, map[string]any{
		"compute_id": "srv-1", "private_network_id": "pn-1", "project": "proj", "region": "par1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputePrivateNetworkDetachMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwPNList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayComputePrivateNetwork(context.Background(), conn, map[string]any{
		"compute_id": "srv-1", "private_network_id": "pn-1", "project": "proj", "region": "par1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputePrivateNetworkBadRegion(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleScalewayComputePrivateNetwork(context.Background(), conn, map[string]any{
		"compute_id": "srv-1", "private_network_id": "pn-1", "project": "proj", "region": "bogus",
	})
	if err == nil {
		t.Fatal("expected error for bad region")
	}
}
