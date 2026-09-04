package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayUserDataSetNewKey(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance user-data list server-id=srv1 zone=fr-par-1 -o json":                     {RC: 0, Stdout: `[]`},
		"scw instance user-data set server-id=srv1 key=cloud-init content=hello zone=fr-par-1": {RC: 0},
	})
	res, err := moduleScalewayUserData(context.Background(), conn, map[string]any{
		"region": "par1", "server_id": "srv1", "user_data": map[string]any{"cloud-init": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayUserDataNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance user-data list server-id=srv1 zone=fr-par-1 -o json":       {RC: 0, Stdout: `["cloud-init"]`},
		"scw instance user-data get server-id=srv1 key=cloud-init zone=fr-par-1": {RC: 0, Stdout: "hello"},
	})
	res, err := moduleScalewayUserData(context.Background(), conn, map[string]any{
		"region": "par1", "server_id": "srv1", "user_data": map[string]any{"cloud-init": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayUserDataDeletesRemovedKey(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance user-data list server-id=srv1 zone=fr-par-1 -o json":     {RC: 0, Stdout: `["stale"]`},
		"scw instance user-data get server-id=srv1 key=stale zone=fr-par-1":    {RC: 0, Stdout: "old"},
		"scw instance user-data delete server-id=srv1 key=stale zone=fr-par-1": {RC: 0},
	})
	res, err := moduleScalewayUserData(context.Background(), conn, map[string]any{
		"region": "par1", "server_id": "srv1", "user_data": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayUserDataRequiresUserData(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v scw": {RC: 0}})
	_, err := moduleScalewayUserData(context.Background(), conn, map[string]any{
		"region": "par1", "server_id": "srv1",
	})
	if err == nil {
		t.Fatal("expected an error when user_data is omitted")
	}
}
