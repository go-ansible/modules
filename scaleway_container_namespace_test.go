package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const scwCNSList = "scw container namespace list project-id=proj region=fr-par -o json"

func TestModuleScalewayContainerNamespaceCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCNSList: {RC: 0, Stdout: `[]`},
		"scw container namespace create name=ns project-id=proj region=fr-par description= -o json": {
			RC: 0, Stdout: `{"id":"ns-1","name":"ns","description":"","environment_variables":{}}`,
		},
	})
	res, err := moduleScalewayContainerNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	ns, ok := res.Extra["container_namespace"].(map[string]any)
	if !ok || ns["id"] != "ns-1" {
		t.Fatalf("container_namespace = %+v", res.Extra["container_namespace"])
	}
}

func TestModuleScalewayContainerNamespaceNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCNSList: {RC: 0, Stdout: `[{"id":"ns-1","name":"ns","description":"hi","environment_variables":{"A":"b"}}]`},
	})
	res, err := moduleScalewayContainerNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par", "description": "hi",
		"environment_variables": map[string]any{"A": "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerNamespaceUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCNSList: {RC: 0, Stdout: `[{"id":"ns-1","name":"ns","description":"old","environment_variables":{}}]`},
		"scw container namespace update namespace-id=ns-1 region=fr-par description=new -o json": {
			RC: 0, Stdout: `{"id":"ns-1","name":"ns","description":"new","environment_variables":{}}`,
		},
	})
	res, err := moduleScalewayContainerNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par", "description": "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerNamespaceDeleteExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCNSList: {RC: 0, Stdout: `[{"id":"ns-1","name":"ns"}]`},
		"scw container namespace delete namespace-id=ns-1 region=fr-par": {RC: 0},
	})
	res, err := moduleScalewayContainerNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerNamespaceDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCNSList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayContainerNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerNamespaceBadRegion(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleScalewayContainerNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "bogus",
	})
	if err == nil {
		t.Fatal("expected error for bad region")
	}
}
