package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayContainerNamespaceInfoFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCNSList: {RC: 0, Stdout: `[{"id":"ns-1","name":"ns"}]`},
	})
	res, err := moduleScalewayContainerNamespaceInfo(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	ns, ok := res.Extra["container_namespace"].(map[string]any)
	if !ok || ns["id"] != "ns-1" {
		t.Fatalf("container_namespace = %+v", res.Extra["container_namespace"])
	}
}

func TestModuleScalewayContainerNamespaceInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwCNSList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayContainerNamespaceInfo(context.Background(), conn, map[string]any{
		"name": "ns", "project_id": "proj", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("expected Failed, res = %+v", res)
	}
}
