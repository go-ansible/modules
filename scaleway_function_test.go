package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const scwFunctionList = "scw function function list namespace-id=ns-1 region=fr-par -o json"

func TestModuleScalewayFunctionCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFunctionList: {RC: 0, Stdout: `[]`},
		"scw function function create name=fn namespace-id=ns-1 region=fr-par runtime=python3 description= privacy=public -o json": {
			RC: 0, Stdout: `{"id":"fn-1","name":"fn","runtime":"python3"}`,
		},
	})
	res, err := moduleScalewayFunction(context.Background(), conn, map[string]any{
		"name": "fn", "namespace_id": "ns-1", "region": "fr-par", "runtime": "python3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayFunctionNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFunctionList: {RC: 0, Stdout: `[{"id":"fn-1","name":"fn","description":"","privacy":"public","runtime":"python3","handler":"","environment_variables":{}}]`},
	})
	res, err := moduleScalewayFunction(context.Background(), conn, map[string]any{
		"name": "fn", "namespace_id": "ns-1", "region": "fr-par", "runtime": "python3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayFunctionDeleteExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFunctionList: {RC: 0, Stdout: `[{"id":"fn-1","name":"fn"}]`},
		"scw function function delete function-id=fn-1 region=fr-par": {RC: 0},
	})
	res, err := moduleScalewayFunction(context.Background(), conn, map[string]any{
		"name": "fn", "namespace_id": "ns-1", "region": "fr-par", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
