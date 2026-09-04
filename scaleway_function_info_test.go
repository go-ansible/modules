package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayFunctionInfoFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFunctionList: {RC: 0, Stdout: `[{"id":"fn-1","name":"fn"}]`},
	})
	res, err := moduleScalewayFunctionInfo(context.Background(), conn, map[string]any{
		"name": "fn", "namespace_id": "ns-1", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayFunctionInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFunctionList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayFunctionInfo(context.Background(), conn, map[string]any{
		"name": "fn", "namespace_id": "ns-1", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("expected Failed, res = %+v", res)
	}
}
