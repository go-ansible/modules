package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayVolumeCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance volume list zone=fr-par-1 -o json": {RC: 0, Stdout: `[]`},
		"scw instance volume create name=data project-id=proj1 volume-type=l_ssd size=10000000000 zone=fr-par-1 -o json": {
			RC: 0, Stdout: `{"id":"vol1","name":"data","project":"proj1"}`,
		},
	})
	res, err := moduleScalewayVolume(context.Background(), conn, map[string]any{
		"name": "data", "region": "par1", "project": "proj1", "size": 10000000000, "volume_type": "l_ssd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayVolumeNoChangeMatchByProjectAndName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance volume list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"other","name":"data","project":"proj2"},{"id":"vol1","name":"data","project":"proj1"}]`,
		},
	})
	res, err := moduleScalewayVolume(context.Background(), conn, map[string]any{
		"name": "data", "region": "par1", "project": "proj1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayVolumeDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance volume list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"vol1","name":"data","project":"proj1"}]`,
		},
		"scw instance volume delete volume-id=vol1 zone=fr-par-1": {RC: 0},
	})
	res, err := moduleScalewayVolume(context.Background(), conn, map[string]any{
		"name": "data", "region": "par1", "project": "proj1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayVolumeMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v scw": {RC: 0}})
	_, err := moduleScalewayVolume(context.Background(), conn, map[string]any{
		"name": "data", "region": "par1", "project": "proj1", "organization": "org1",
	})
	if err == nil {
		t.Fatal("expected an error when both project and organization are given")
	}
}
