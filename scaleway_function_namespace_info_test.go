package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayFunctionNamespaceInfoFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFNSList: {RC: 0, Stdout: `[{"id":"ns-1","name":"ns"}]`},
	})
	res, err := moduleScalewayFunctionNamespaceInfo(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayFunctionNamespaceInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwFNSList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayFunctionNamespaceInfo(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("expected Failed, res = %+v", res)
	}
}
