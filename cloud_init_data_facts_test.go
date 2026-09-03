package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleCloudInitDataFactsBoth(t *testing.T) {
	resultTest := "test -e /var/lib/cloud/data/result.json"
	resultCat := "cat /var/lib/cloud/data/result.json"
	statusTest := "test -e /var/lib/cloud/data/status.json"
	statusCat := "cat /var/lib/cloud/data/status.json"
	fc := newFakeConn(map[string]remoteexec.Result{
		resultTest: {RC: 0},
		resultCat:  {RC: 0, Stdout: `{"v1":{"datasource":"DataSourceCloudStack"}}`},
		statusTest: {RC: 0},
		statusCat:  {RC: 0, Stdout: `{"v1":{"datasource":"DataSourceCloudStack","errors":[]}}`},
	})
	res, err := moduleCloudInitDataFacts(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, ok := res.Facts["cloud_init_data_facts"].(map[string]any)
	if !ok {
		t.Fatalf("Facts[cloud_init_data_facts] = %#v", res.Facts["cloud_init_data_facts"])
	}
	if _, ok := facts["result"]; !ok {
		t.Fatalf("facts = %+v, want a result key", facts)
	}
	if _, ok := facts["status"]; !ok {
		t.Fatalf("facts = %+v, want a status key", facts)
	}
	// Same data must also be surfaced at the top level via Extra, matching
	// real cloud_init_data_facts' own dual ansible_facts+top-level return.
	extra, ok := res.Extra["cloud_init_data_facts"].(map[string]any)
	if !ok {
		t.Fatalf("Extra[cloud_init_data_facts] = %#v", res.Extra["cloud_init_data_facts"])
	}
	if extra["result"] == nil {
		t.Fatalf("extra = %+v", extra)
	}
}

func TestModuleCloudInitDataFactsFilterStatus(t *testing.T) {
	statusTest := "test -e /var/lib/cloud/data/status.json"
	statusCat := "cat /var/lib/cloud/data/status.json"
	fc := newFakeConn(map[string]remoteexec.Result{
		statusTest: {RC: 0},
		statusCat:  {RC: 0, Stdout: `{"v1":{"datasource":"DataSourceCloudStack"}}`},
	})
	res, err := moduleCloudInitDataFacts(context.Background(), fc, map[string]any{"filter": "status"})
	if err != nil {
		t.Fatal(err)
	}
	facts := res.Facts["cloud_init_data_facts"].(map[string]any)
	if _, ok := facts["result"]; ok {
		t.Fatalf("facts = %+v, want NO result key when filter=status", facts)
	}
	if len(fc.Commands) != 2 {
		t.Fatalf("commands = %v, want only the status.json check+read", fc.Commands)
	}
}

func TestModuleCloudInitDataFactsMissingFilesAreEmpty(t *testing.T) {
	resultTest := "test -e /var/lib/cloud/data/result.json"
	statusTest := "test -e /var/lib/cloud/data/status.json"
	fc := newFakeConn(map[string]remoteexec.Result{
		resultTest: {RC: 1},
		statusTest: {RC: 1},
	})
	res, err := moduleCloudInitDataFacts(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts := res.Facts["cloud_init_data_facts"].(map[string]any)
	resultVal, ok := facts["result"].(map[string]any)
	if !ok || len(resultVal) != 0 {
		t.Fatalf("facts[result] = %#v, want an empty map when the file is absent", facts["result"])
	}
}
