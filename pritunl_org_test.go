package modules

import (
	"context"
	"testing"
)

func TestModulePritunlOrgFailsLoud(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := modulePritunlOrg(context.Background(), conn, map[string]any{"name": "MyOrg"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: pritunl CLI has no org/user management support")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("expected no commands run, got %v", conn.Commands)
	}
}
