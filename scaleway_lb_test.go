package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayLBCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw lb lb list name=foobar zone=fr-par-1 -o json": {RC: 0, Stdout: `[]`},
		"scw lb lb create name=foobar description=desc organization-id=org1 tags.0=hello zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"lb1","name":"foobar","description":"desc"}`,
		},
	})
	res, err := moduleScalewayLB(context.Background(), conn, map[string]any{
		"name": "foobar", "description": "desc", "organization_id": "org1", "region": "fr-par", "tags": []any{"hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	lb, ok := res.Extra["lb"].(map[string]any)
	if !ok || lb["id"] != "lb1" {
		t.Fatalf("lb = %+v", res.Extra["lb"])
	}
}

func TestModuleScalewayLBUpdateDescription(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw lb lb list name=foobar zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"lb1","name":"foobar","description":"old"}]`,
		},
		"scw lb lb update lb-id=lb1 name=foobar description=new zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"lb1","name":"foobar","description":"new"}`,
		},
	})
	res, err := moduleScalewayLB(context.Background(), conn, map[string]any{
		"name": "foobar", "description": "new", "organization_id": "org1", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayLBNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw lb lb list name=foobar zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"lb1","name":"foobar","description":"desc"}]`,
		},
	})
	res, err := moduleScalewayLB(context.Background(), conn, map[string]any{
		"name": "foobar", "description": "desc", "organization_id": "org1", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayLBDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw lb lb list name=foobar zone=nl-ams-1 -o json": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayLB(context.Background(), conn, map[string]any{
		"name": "foobar", "description": "desc", "organization_id": "org1", "region": "nl-ams", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayLBBadRegion(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v scw": {RC: 0}})
	_, err := moduleScalewayLB(context.Background(), conn, map[string]any{
		"name": "foobar", "description": "desc", "organization_id": "org1", "region": "par1",
	})
	if err == nil {
		t.Fatal("expected an error for a zone-shaped region on scaleway_lb")
	}
}
