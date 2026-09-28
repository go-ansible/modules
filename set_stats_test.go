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
	// Real reports it under ansible_stats, wrapped with the two options
	// that decide aggregation, and carries no msg -- measured:
	// {'data': {...}, 'per_host': False, 'aggregate': True}.
	if _, gone := res.Extra["set_stats"]; gone {
		t.Error("set_stats key present; real reports ansible_stats")
	}
	if !res.NoMsg {
		t.Error("a msg key is present; real has none")
	}
	wrapper, ok := res.Extra["ansible_stats"].(map[string]any)
	if !ok {
		t.Fatalf("ansible_stats = %#v", res.Extra["ansible_stats"])
	}
	if wrapper["aggregate"] != true || wrapper["per_host"] != false {
		t.Errorf("wrapper = %#v, want aggregate true and per_host false", wrapper)
	}
	stats, ok := wrapper["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", wrapper["data"])
	}
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
