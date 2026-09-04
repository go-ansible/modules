package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayPrivateNetworkCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw vpc private-network list name=vpc_one region=fr-par -o json": {RC: 0, Stdout: `[]`},
		"scw vpc private-network create name=vpc_one project-id=proj1 region=fr-par -o json": {
			RC: 0, Stdout: `{"id":"pn1","name":"vpc_one","tags":[]}`,
		},
	})
	res, err := moduleScalewayPrivateNetwork(context.Background(), conn, map[string]any{
		"project": "proj1", "name": "vpc_one", "region": "par1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayPrivateNetworkNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw vpc private-network list name=vpc_one region=fr-par -o json": {
			RC: 0, Stdout: `[{"id":"pn1","name":"vpc_one","tags":["a","b"]}]`,
		},
	})
	res, err := moduleScalewayPrivateNetwork(context.Background(), conn, map[string]any{
		"project": "proj1", "name": "vpc_one", "region": "par1", "tags": []any{"b", "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayPrivateNetworkUpdateTags(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw vpc private-network list name=vpc_one region=fr-par -o json": {
			RC: 0, Stdout: `[{"id":"pn1","name":"vpc_one","tags":["a"]}]`,
		},
		"scw vpc private-network update private-network-id=pn1 name=vpc_one tags.0=b region=fr-par -o json": {
			RC: 0, Stdout: `{"id":"pn1","name":"vpc_one","tags":["b"]}`,
		},
	})
	res, err := moduleScalewayPrivateNetwork(context.Background(), conn, map[string]any{
		"project": "proj1", "name": "vpc_one", "region": "par1", "tags": []any{"b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayPrivateNetworkAbsentNoName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v scw": {RC: 0}})
	res, err := moduleScalewayPrivateNetwork(context.Background(), conn, map[string]any{
		"project": "proj1", "region": "par1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
