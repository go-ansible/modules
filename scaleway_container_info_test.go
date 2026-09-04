package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayContainerInfoFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwContainerList: {RC: 0, Stdout: `[{"id":"cn-1","name":"cn"}]`},
	})
	res, err := moduleScalewayContainerInfo(context.Background(), conn, map[string]any{
		"name": "cn", "namespace_id": "ns-1", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwContainerList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayContainerInfo(context.Background(), conn, map[string]any{
		"name": "cn", "namespace_id": "ns-1", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("expected Failed, res = %+v", res)
	}
}
