package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRebootConnectionDrop(t *testing.T) {
	// failAfterConn{n:0} fails every Exec call, simulating the
	// connection dropping mid-reboot-command — the expected outcome.
	conn := &failAfterConn{n: 0}
	res, err := moduleReboot(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRebootCleanReturn(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"reboot": {RC: 0},
	})
	res, err := moduleReboot(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRebootCustomCommand(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"shutdown -r now": {RC: 0},
	})
	res, err := moduleReboot(context.Background(), conn, map[string]any{"reboot_command": "shutdown -r now"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "shutdown -r now" {
		t.Fatalf("Commands = %v", conn.Commands)
	}
}
