package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const scwContainerList = "scw container container list namespace-id=ns-1 region=fr-par -o json"

func TestModuleScalewayContainerCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwContainerList: {RC: 0, Stdout: `[]`},
		"scw container container create name=cn namespace-id=ns-1 region=fr-par registry-image=nginx:latest description= privacy=public protocol=http1 -o json": {
			RC: 0, Stdout: `{"id":"cn-1","name":"cn","registry_image":"nginx:latest"}`,
		},
	})
	res, err := moduleScalewayContainer(context.Background(), conn, map[string]any{
		"name": "cn", "namespace_id": "ns-1", "region": "fr-par", "registry_image": "nginx:latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwContainerList: {RC: 0, Stdout: `[{"id":"cn-1","name":"cn","description":"","privacy":"public","protocol":"http1","registry_image":"nginx:latest","environment_variables":{}}]`},
	})
	res, err := moduleScalewayContainer(context.Background(), conn, map[string]any{
		"name": "cn", "namespace_id": "ns-1", "region": "fr-par", "registry_image": "nginx:latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayContainerUpdateWithRedeploy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwContainerList: {RC: 0, Stdout: `[{"id":"cn-1","name":"cn","description":"old","privacy":"public","protocol":"http1","registry_image":"nginx:latest","environment_variables":{}}]`},
		"scw container container update container-id=cn-1 region=fr-par registry-image=nginx:latest description=new privacy=public protocol=http1 -o json": {
			RC: 0, Stdout: `{"id":"cn-1","name":"cn","description":"new"}`,
		},
		"scw container container deploy container-id=cn-1 region=fr-par -o json": {
			RC: 0, Stdout: `{"id":"cn-1","name":"cn","description":"new","status":"deploying"}`,
		},
	})
	res, err := moduleScalewayContainer(context.Background(), conn, map[string]any{
		"name": "cn", "namespace_id": "ns-1", "region": "fr-par", "registry_image": "nginx:latest",
		"description": "new", "redeploy": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	cn, ok := res.Extra["container"].(map[string]any)
	if !ok || cn["status"] != "deploying" {
		t.Fatalf("container = %+v", res.Extra["container"])
	}
}

func TestModuleScalewayContainerDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwContainerList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayContainer(context.Background(), conn, map[string]any{
		"name": "cn", "namespace_id": "ns-1", "region": "fr-par", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
