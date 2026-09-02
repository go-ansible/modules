package modules

import (
	"context"
	"testing"
)

func TestModuleAsyncStatusFailsHonestly(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleAsyncStatus(context.Background(), conn, map[string]any{"jid": "12345.6789"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: async is not implemented in this port")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no commands run, got %v", conn.Commands)
	}
}

func TestModuleAsyncStatusCleanupModeAlsoFails(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleAsyncStatus(context.Background(), conn, map[string]any{"jid": "1", "mode": "cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}

func TestModuleAsyncStatusMissingJid(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAsyncStatus(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing jid")
	}
}

func TestModuleAsyncStatusInvalidMode(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAsyncStatus(context.Background(), conn, map[string]any{"jid": "1", "mode": "bogus"}); err == nil {
		t.Fatal("want error for invalid mode")
	}
}
