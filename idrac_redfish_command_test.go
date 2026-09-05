package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIdracRedfishCommandCreateBiosConfigJob(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v racadm": {RC: 0},
		"racadm jobqueue create BIOS.Setup.1-1 -r pwrcycle -s TIME_NOW": {
			RC: 0, Stdout: "Job ID = JID_471269252011\nCommit JID = JID_471269252011",
		},
	})
	args := map[string]any{
		"category": "Systems", "command": []any{"CreateBiosConfigJob"},
		"resource_id": "System.Embedded.1", "baseuri": "10.0.0.1", "username": "root", "password": "x",
	}
	res, err := moduleIdracRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	rv, _ := res.Extra["return_values"].(map[string]any)
	if rv["job_id"] != "JID_471269252011" {
		t.Fatalf("job_id = %v", rv["job_id"])
	}
}

func TestModuleIdracRedfishCommandInvalidCategory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	args := map[string]any{"category": "Bogus", "command": []any{"CreateBiosConfigJob"}, "baseuri": "x"}
	res, err := moduleIdracRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleIdracRedfishCommandInvalidCommand(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	args := map[string]any{"category": "Systems", "command": []any{"Bogus"}, "baseuri": "x"}
	res, err := moduleIdracRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
