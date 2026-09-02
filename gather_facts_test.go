package modules

import (
	"context"
	"testing"
)

func TestModuleGatherFactsDelegatesToSetup(t *testing.T) {
	res, err := moduleGatherFacts(context.Background(), local(), map[string]any{"parallel": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Facts["hostname"] == "" || res.Facts["hostname"] == nil {
		t.Fatalf("Facts[hostname] missing: %#v", res.Facts)
	}
}
