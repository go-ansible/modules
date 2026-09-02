package modules

import (
	"context"
	"testing"
)

// moduleSetup delegates to github.com/go-ansible/facts, which already
// has its own thorough real-connection test (facts_test.go's
// TestGatherLocal). Here we only need to check the delegation itself:
// the module wraps Gather's map into Result.Facts.
func TestModuleSetupGathersRealLocalFacts(t *testing.T) {
	res, err := moduleSetup(context.Background(), local(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Facts["hostname"] == "" || res.Facts["hostname"] == nil {
		t.Fatalf("Facts[hostname] missing: %#v", res.Facts)
	}
	if res.Facts["system"] == "" || res.Facts["system"] == nil {
		t.Fatalf("Facts[system] missing: %#v", res.Facts)
	}
}

func TestModuleSetupIgnoresUnsupportedArgs(t *testing.T) {
	// fact_path/filter/gather_subset are accepted but not implemented;
	// passing them must not error.
	res, err := moduleSetup(context.Background(), local(), map[string]any{
		"fact_path":     "/etc/ansible/facts.d",
		"filter":        []string{"ansible_hostname"},
		"gather_subset": []string{"all"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
