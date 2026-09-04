package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayIPCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance ip list zone=fr-par-1 -o json": {RC: 0, Stdout: `[]`},
		"scw instance ip create project-id=proj1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"ip1","address":"1.2.3.4","reverse":null,"server":null}`,
		},
	})
	res, err := moduleScalewayIP(context.Background(), conn, map[string]any{
		"project": "proj1", "region": "par1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	ip, ok := res.Extra["ip"].(map[string]any)
	if !ok || ip["id"] != "ip1" {
		t.Fatalf("ip = %+v", res.Extra["ip"])
	}
}

func TestModuleScalewayIPAttachExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance ip list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"ip1","reverse":"","server":null}]`,
		},
		"scw instance ip attach ip1 server=srv1 zone=fr-par-1 -o json": {RC: 0, Stdout: `{"id":"ip1"}`},
		"scw instance ip get ip1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"ip1","reverse":"","server":{"id":"srv1"}}`,
		},
	})
	res, err := moduleScalewayIP(context.Background(), conn, map[string]any{
		"project": "proj1", "region": "par1", "id": "ip1", "server": "srv1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayIPNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance ip list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"ip1","reverse":"","server":null}]`,
		},
		"scw instance ip get ip1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"ip1","reverse":"","server":null}`,
		},
	})
	res, err := moduleScalewayIP(context.Background(), conn, map[string]any{
		"project": "proj1", "region": "par1", "id": "ip1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayIPDeleteExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance ip list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"ip1"}]`,
		},
		"scw instance ip delete ip=ip1 zone=fr-par-1": {RC: 0},
	})
	res, err := moduleScalewayIP(context.Background(), conn, map[string]any{
		"project": "proj1", "region": "par1", "id": "ip1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayIPDeleteMissingID(t *testing.T) {
	// Real scaleway_ip's own absent_strategy always lists first (GET
	// /ips), even when id is unset — an unset id can never match a
	// real IP, so this is always Changed=false, but the list call
	// still happens.
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance ip list zone=fr-par-1 -o json": {RC: 0, Stdout: `[{"id":"other"}]`},
	})
	res, err := moduleScalewayIP(context.Background(), conn, map[string]any{
		"project": "proj1", "region": "par1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("expected probe+list only (no delete), got %v", conn.Commands)
	}
}

func TestModuleScalewayIPRequiresExactlyOneOfOrgProject(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleScalewayIP(context.Background(), conn, map[string]any{
		"region": "par1",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
}
