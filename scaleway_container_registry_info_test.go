package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayContainerRegistryInfoFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCRList: {RC: 0, Stdout: `[{"id":"reg-1","name":"reg"}]`},
	})
	res, err := moduleScalewayContainerRegistryInfo(context.Background(), conn, map[string]any{
		"name": "reg", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerRegistryInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCRList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayContainerRegistryInfo(context.Background(), conn, map[string]any{
		"name": "reg", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("expected Failed, res = %+v", res)
	}
}
