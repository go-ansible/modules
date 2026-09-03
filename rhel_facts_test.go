package modules

import (
	"context"
	"testing"
)

func TestModuleRhelFacts(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleRhelFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want a plain success, got %+v", res)
	}
	if res.Facts["pkg_mgr"] != "ansible.posix.rhel_facts" {
		t.Fatalf("Facts[pkg_mgr] = %v", res.Facts["pkg_mgr"])
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no commands issued, got %v", conn.Commands)
	}
}
