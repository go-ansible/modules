package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacketProjectCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v metal":                        {RC: 0},
		"metal project get -o json":               {RC: 0, Stdout: `{"projects":[]}`},
		"metal project create -n my-proj -o json": {RC: 0, Stdout: `{"id":"proj-1","name":"my-proj"}`},
	})
	res, err := modulePacketProject(context.Background(), conn, map[string]any{"name": "my-proj"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "proj-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModulePacketProjectDeleteByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v metal":                          {RC: 0},
		"metal project get -o json":                 {RC: 0, Stdout: `{"projects":[{"id":"proj-1","name":"my-proj"}]}`},
		"metal project delete -i proj-1 -f -o json": {RC: 0},
	})
	args := map[string]any{"id": "proj-1", "state": "absent"}
	res, err := modulePacketProject(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
