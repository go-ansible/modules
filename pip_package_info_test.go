package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const pipListSample = `[{"name": "Babel", "version": "2.6.0"}, {"name": "Flask", "version": "1.0.2"}]`

func TestModulePipPackageInfoDefault(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip list -l --format=json": {RC: 0, Stdout: pipListSample},
	})
	res, err := modulePipPackageInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("pip_package_info must never report Changed")
	}
	packages := res.Extra["packages"].(map[string]any)
	pip := packages["pip"].(map[string]any)
	babel := pip["Babel"].([]any)[0].(map[string]any)
	if babel["name"] != "Babel" || babel["version"] != "2.6.0" || babel["source"] != "pip" {
		t.Fatalf("babel = %v", babel)
	}
}

func TestModulePipPackageInfoMultipleClients(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip list -l --format=json":    {RC: 0, Stdout: pipListSample},
		"pip3.6 list -l --format=json": {RC: 0, Stdout: `[{"name": "Jinja2", "version": "2.10"}]`},
	})
	res, err := modulePipPackageInfo(context.Background(), conn, map[string]any{
		"clients": []any{"pip", "pip3.6"},
	})
	if err != nil {
		t.Fatal(err)
	}
	packages := res.Extra["packages"].(map[string]any)
	if len(packages) != 2 {
		t.Fatalf("packages = %v", packages)
	}
}

func TestModulePipPackageInfoSkipsInvalidClient(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip list -l --format=json": {RC: 0, Stdout: pipListSample},
	})
	res, err := modulePipPackageInfo(context.Background(), conn, map[string]any{
		"clients": []any{"pip", "notpip"},
	})
	if err != nil {
		t.Fatal(err)
	}
	skipped := res.Extra["skipped_clients"].([]string)
	if len(skipped) != 1 || skipped[0] != "notpip" {
		t.Fatalf("skipped_clients = %v", skipped)
	}
}

func TestModulePipPackageInfoAllClientsFail(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip list -l --format=json": {RC: 127, Stderr: "command not found"},
	})
	res, err := modulePipPackageInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when every client fails")
	}
}
