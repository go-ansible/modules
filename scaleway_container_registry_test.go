package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const scwCRList = "scw registry namespace list project-id=proj region=fr-par -o json"

func TestModuleScalewayContainerRegistryCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCRList: {RC: 0, Stdout: `[]`},
		"scw registry namespace create name=reg project-id=proj region=fr-par description= is-public=false -o json": {
			RC: 0, Stdout: `{"id":"reg-1","name":"reg","description":"","is_public":false}`,
		},
	})
	res, err := moduleScalewayContainerRegistry(context.Background(), conn, map[string]any{
		"name": "reg", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerRegistryNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCRList: {RC: 0, Stdout: `[{"id":"reg-1","name":"reg","description":"","is_public":true}]`},
	})
	res, err := moduleScalewayContainerRegistry(context.Background(), conn, map[string]any{
		"name": "reg", "project_id": "proj", "region": "fr-par", "privacy_policy": "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerRegistryUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCRList: {RC: 0, Stdout: `[{"id":"reg-1","name":"reg","description":"","is_public":false}]`},
		"scw registry namespace update namespace-id=reg-1 region=fr-par description= is-public=true -o json": {
			RC: 0, Stdout: `{"id":"reg-1","name":"reg","description":"","is_public":true}`,
		},
	})
	res, err := moduleScalewayContainerRegistry(context.Background(), conn, map[string]any{
		"name": "reg", "project_id": "proj", "region": "fr-par", "privacy_policy": "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerRegistryDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCRList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayContainerRegistry(context.Background(), conn, map[string]any{
		"name": "reg", "project_id": "proj", "region": "fr-par", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
