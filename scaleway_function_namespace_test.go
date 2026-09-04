package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const scwFNSList = "scw function namespace list project-id=proj region=fr-par -o json"

func TestModuleScalewayFunctionNamespaceCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFNSList: {RC: 0, Stdout: `[]`},
		"scw function namespace create name=ns project-id=proj region=fr-par description= -o json": {
			RC: 0, Stdout: `{"id":"ns-1","name":"ns","description":"","environment_variables":{}}`,
		},
	})
	res, err := moduleScalewayFunctionNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayFunctionNamespaceNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFNSList: {RC: 0, Stdout: `[{"id":"ns-1","name":"ns","description":"hi","environment_variables":{}}]`},
	})
	res, err := moduleScalewayFunctionNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par", "description": "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayFunctionNamespaceDeleteExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFNSList: {RC: 0, Stdout: `[{"id":"ns-1","name":"ns"}]`},
		"scw function namespace delete namespace-id=ns-1 region=fr-par": {RC: 0},
	})
	res, err := moduleScalewayFunctionNamespace(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
