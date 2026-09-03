package modules

import (
	"context"
	"testing"
)

func TestModuleSynchronizeAlwaysFails(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleSynchronize(context.Background(), conn, map[string]any{
		"src": "/local/dir/", "dest": "/remote/dir/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want a clean, documented failure — synchronize is not implemented in this port")
	}
	if res.Msg == "" {
		t.Fatal("want a non-empty explanatory message")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no commands issued, got %v", conn.Commands)
	}
}
