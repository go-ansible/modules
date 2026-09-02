package modules

import (
	"context"
	"testing"
)

func TestModuleSetStats(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSetStats(context.Background(), conn, map[string]any{
		"data": map[string]any{"errors": 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	stats := res.Extra["set_stats"].(map[string]any)
	if stats["errors"] != 3 {
		t.Fatalf("stats = %#v", stats)
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no target commands, got %v", conn.Commands)
	}
}

func TestModuleSetStatsMissingData(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSetStats(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing data")
	}
}

func TestModuleSetStatsWrongType(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSetStats(context.Background(), conn, map[string]any{"data": "not a map"}); err == nil {
		t.Fatal("want error for non-map data")
	}
}
