package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAirbrakeDeploymentSuccess(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v airbrake": {RC: 0},
		"airbrake deploys create --project-id 12345 --environment staging --username ansible --revision 4.2": {RC: 0},
	})
	res, err := moduleAirbrakeDeployment(context.Background(), conn, map[string]any{
		"project_id":  "12345",
		"project_key": "AAAAAA",
		"environment": "staging",
		"user":        "ansible",
		"revision":    "4.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed; res = %+v", res)
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleAirbrakeDeploymentAllFields(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v airbrake": {RC: 0},
		"airbrake deploys create --project-id 12345 --environment staging --username ansible --repository https://github.com/x/y --revision abc123 --version 1.2.3": {RC: 0},
	})
	res, err := moduleAirbrakeDeployment(context.Background(), conn, map[string]any{
		"project_id":  "12345",
		"project_key": "AAAAAA",
		"environment": "staging",
		"user":        "ansible",
		"repo":        "https://github.com/x/y",
		"revision":    "abc123",
		"version":     "1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleAirbrakeDeploymentFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v airbrake": {RC: 0},
		"airbrake deploys create --project-id 12345 --environment staging": {RC: 1, Stderr: "401 Unauthorized"},
	})
	res, err := moduleAirbrakeDeployment(context.Background(), conn, map[string]any{
		"project_id":  "12345",
		"project_key": "AAAAAA",
		"environment": "staging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleAirbrakeDeploymentMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v airbrake": {RC: 1},
	})
	res, err := moduleAirbrakeDeployment(context.Background(), conn, map[string]any{
		"project_id":  "12345",
		"project_key": "AAAAAA",
		"environment": "staging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleAirbrakeDeploymentCustomURLRejected(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v airbrake": {RC: 0},
	})
	res, err := moduleAirbrakeDeployment(context.Background(), conn, map[string]any{
		"project_id":  "12345",
		"project_key": "AAAAAA",
		"environment": "staging",
		"url":         "https://errbit.example.com/api/v4/projects/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed for unsupported custom url, res = %+v", res)
	}
}

func TestModuleAirbrakeDeploymentMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleAirbrakeDeployment(context.Background(), conn, map[string]any{"project_id": "12345"})
	if err == nil {
		t.Fatal("want error for missing required args")
	}
}
