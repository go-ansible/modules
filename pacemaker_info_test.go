package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacemakerInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pcs --version": {RC: 0, Stdout: "0.11.7\n"},
		"pcs cluster config --output-format=json":    {RC: 0, Stdout: `{"name": "mycluster"}`},
		"pcs constraint config --output-format=json": {RC: 0, Stdout: `{"location": []}`},
		"pcs property config --output-format=json":   {RC: 0, Stdout: `{"maintenance-mode": "false"}`},
		"pcs resource config --output-format=json":   {RC: 0, Stdout: `{"resources": []}`},
		"pcs stonith config --output-format=json":    {RC: 0, Stdout: `{"stonith": []}`},
	})
	res, err := modulePacemakerInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("pacemaker_info must never report Changed")
	}
	if res.Extra["version"] != "0.11.7" {
		t.Fatalf("version = %v", res.Extra["version"])
	}
	ci := res.Extra["cluster_info"].(map[string]any)
	if ci["name"] != "mycluster" {
		t.Fatalf("cluster_info = %v", ci)
	}
	if res.Extra["constraint_info"] == nil || res.Extra["property_info"] == nil ||
		res.Extra["resource_info"] == nil || res.Extra["stonith_info"] == nil {
		t.Fatalf("missing a section: %+v", res.Extra)
	}
}

func TestModulePacemakerInfoVersionFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pcs --version": {RC: 127, Stderr: "pcs: command not found"},
	})
	res, err := modulePacemakerInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when pcs --version fails")
	}
}

func TestModulePacemakerInfoSectionFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pcs --version": {RC: 0, Stdout: "0.11.7\n"},
		"pcs cluster config --output-format=json": {RC: 1, Stderr: "Error: unable to get cluster status"},
	})
	res, err := modulePacemakerInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when any section query fails")
	}
}

func TestModulePacemakerInfoInvalidJSON(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pcs --version": {RC: 0, Stdout: "0.11.7\n"},
		"pcs cluster config --output-format=json": {RC: 0, Stdout: "not json"},
	})
	res, err := modulePacemakerInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for unparseable JSON output")
	}
}
