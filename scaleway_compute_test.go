package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const scwServerList = "scw instance server list name=srv zone=fr-par-1 -o json"

func TestModuleScalewayComputeCreatePresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwServerList: {RC: 0, Stdout: `[]`},
		"scw instance server create image=img-1 type=DEV1-S name=srv zone=fr-par-1 dynamic-ip-required=false project-id=proj -o json": {
			RC: 0, Stdout: `{"id":"srv-1","name":"srv","state":"stopped","tags":[]}`,
		},
	})
	res, err := moduleScalewayCompute(context.Background(), conn, map[string]any{
		"image": "img-1", "commercial_type": "DEV1-S", "name": "srv", "region": "par1", "project": "proj",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	srv, ok := res.Extra["server"].(map[string]any)
	if !ok || srv["id"] != "srv-1" {
		t.Fatalf("server = %+v", res.Extra["server"])
	}
}

func TestModuleScalewayComputePresentNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwServerList: {RC: 0, Stdout: `[{"id":"srv-1","name":"srv","state":"running","tags":[]}]`},
	})
	res, err := moduleScalewayCompute(context.Background(), conn, map[string]any{
		"image": "img-1", "commercial_type": "DEV1-S", "name": "srv", "region": "par1", "project": "proj",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputeRunningStartsStoppedServer(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwServerList: {RC: 0, Stdout: `[{"id":"srv-1","name":"srv","state":"stopped","tags":[]}]`},
		"scw instance server start srv-1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"srv-1","name":"srv","state":"starting"}`,
		},
	})
	res, err := moduleScalewayCompute(context.Background(), conn, map[string]any{
		"image": "img-1", "commercial_type": "DEV1-S", "name": "srv", "region": "par1", "project": "proj",
		"state": "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputeRestartedAlwaysChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwServerList: {RC: 0, Stdout: `[{"id":"srv-1","name":"srv","state":"running","tags":[]}]`},
		"scw instance server reboot srv-1 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"srv-1","name":"srv","state":"running"}`,
		},
	})
	res, err := moduleScalewayCompute(context.Background(), conn, map[string]any{
		"image": "img-1", "commercial_type": "DEV1-S", "name": "srv", "region": "par1", "project": "proj",
		"state": "restarted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputeAbsentStopsThenDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwServerList: {RC: 0, Stdout: `[{"id":"srv-1","name":"srv","state":"running","tags":[]}]`},
		"scw instance server stop srv-1 zone=fr-par-1":   {RC: 0},
		"scw instance server delete srv-1 zone=fr-par-1": {RC: 0},
	})
	res, err := moduleScalewayCompute(context.Background(), conn, map[string]any{
		"image": "img-1", "commercial_type": "DEV1-S", "name": "srv", "region": "par1", "project": "proj",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputeAbsentMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		scwServerList: {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewayCompute(context.Background(), conn, map[string]any{
		"image": "img-1", "commercial_type": "DEV1-S", "name": "srv", "region": "par1", "project": "proj",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayComputeRequiresOrgOrProject(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleScalewayCompute(context.Background(), conn, map[string]any{
		"image": "img-1", "commercial_type": "DEV1-S", "name": "srv", "region": "par1",
	})
	if err == nil {
		t.Fatal("expected error when neither organization nor project is given")
	}
}

func TestModuleScalewayComputeOrgAndProjectMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleScalewayCompute(context.Background(), conn, map[string]any{
		"image": "img-1", "commercial_type": "DEV1-S", "name": "srv", "region": "par1",
		"organization": "org", "project": "proj",
	})
	if err == nil {
		t.Fatal("expected error when both organization and project are given")
	}
}
