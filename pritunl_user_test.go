package modules

import (
	"context"
	"testing"
)

func TestModulePritunlUserFailsLoud(t *testing.T) {
	conn := newFakeConn(nil)
	args := map[string]any{"organization": "MyOrg", "user_name": "Foo"}
	res, err := modulePritunlUser(context.Background(), conn, args)
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
