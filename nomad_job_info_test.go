package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNomadJobInfoMissingHost(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleNomadJobInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing host")
	}
}

func TestModuleNomadJobInfoByName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job status awx -json -address=https://localhost:4646": {RC: 0, Stdout: `{"ID":"awx","Status":"running"}`},
	})
	res, err := moduleNomadJobInfo(context.Background(), conn, map[string]any{
		"host": "localhost", "name": "awx",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	result := res.Extra["result"].([]any)
	if len(result) != 1 {
		t.Fatalf("result = %v", result)
	}
}

func TestModuleNomadJobInfoList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job status -json -address=https://localhost:4646": {RC: 0, Stdout: `[{"ID":"awx"},{"ID":"api"}]`},
	})
	res, err := moduleNomadJobInfo(context.Background(), conn, map[string]any{
		"host": "localhost",
	})
	if err != nil {
		t.Fatal(err)
	}
	result := res.Extra["result"].([]any)
	if len(result) != 2 {
		t.Fatalf("result = %v", result)
	}
}

func TestModuleNomadJobInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job status missing -json -address=https://localhost:4646": {RC: 1, Stderr: "job not found"},
	})
	res, err := moduleNomadJobInfo(context.Background(), conn, map[string]any{
		"host": "localhost", "name": "missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}
