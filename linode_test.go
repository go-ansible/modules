package modules

import (
	"context"
	"testing"
)

func TestModuleLinodeAlwaysFailsLoud(t *testing.T) {
	conn := newFakeConn(nil)
	for _, state := range []string{"present", "absent", "active"} {
		res, err := moduleLinode(context.Background(), conn, map[string]any{"name": "x", "state": state})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Failed {
			t.Fatalf("state=%s: want Failed: linode v3 API is permanently retired", state)
		}
	}
}
