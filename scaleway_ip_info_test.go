package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayIPInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance ip list zone=nl-ams-1 -o json": {
			RC: 0, Stdout: `[{"id":"ip1","address":"1.2.3.4"}]`,
		},
	})
	res, err := moduleScalewayIPInfo(context.Background(), conn, map[string]any{"region": "ams1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	ips, ok := res.Extra["ips"].([]map[string]any)
	if !ok || len(ips) != 1 || ips[0]["id"] != "ip1" {
		t.Fatalf("ips = %+v", res.Extra["ips"])
	}
}

func TestModuleScalewayIPInfoEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance ip list zone=fr-par-1 -o json": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayIPInfo(context.Background(), conn, map[string]any{"region": "par1"})
	if err != nil {
		t.Fatal(err)
	}
	ips, ok := res.Extra["ips"].([]map[string]any)
	if !ok || len(ips) != 0 {
		t.Fatalf("ips = %+v", res.Extra["ips"])
	}
}

func TestModuleScalewayIPInfoBadRegion(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v scw": {RC: 0}})
	_, err := moduleScalewayIPInfo(context.Background(), conn, map[string]any{"region": "mars1"})
	if err == nil {
		t.Fatal("expected an error for an unrecognized region")
	}
}
