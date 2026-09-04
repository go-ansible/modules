package modules

import (
	"context"
	"testing"
)

func TestModulePritunlUserInfoFailsLoud(t *testing.T) {
	conn := newFakeConn(nil)
	args := map[string]any{"organization": "MyOrg"}
	res, err := modulePritunlUserInfo(context.Background(), conn, args)
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
