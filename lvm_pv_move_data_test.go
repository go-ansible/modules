package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLvmPvMoveData(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_used --units b --nosuffix /dev/sdb 2>/dev/null": {RC: 0, Stdout: "536870912\n"},
		"pvmove --atomic --autobackup y /dev/sdb /dev/sdc":                      {RC: 0},
	})
	res, err := moduleLvmPvMoveData(context.Background(), conn, map[string]any{
		"source": "/dev/sdb", "destination": "/dev/sdc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvmPvMoveDataNothingToMove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_used --units b --nosuffix /dev/sdb 2>/dev/null": {RC: 0, Stdout: "0\n"},
	})
	res, err := moduleLvmPvMoveData(context.Background(), conn, map[string]any{
		"source": "/dev/sdb", "destination": "/dev/sdc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no pvmove attempted, commands = %v", conn.Commands)
	}
}

func TestModuleLvmPvMoveDataOptions(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pvs --noheadings -o pv_used --units b --nosuffix /dev/sdb 2>/dev/null": {RC: 0, Stdout: "1024\n"},
		"pvmove -y --autobackup n /dev/sdb /dev/sdc":                            {RC: 0},
	})
	res, err := moduleLvmPvMoveData(context.Background(), conn, map[string]any{
		"source": "/dev/sdb", "destination": "/dev/sdc",
		"atomic": false, "auto_answer": true, "autobackup": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLvmPvMoveDataMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLvmPvMoveData(context.Background(), conn, map[string]any{"source": "/dev/sdb"}); err == nil {
		t.Fatal("want error for missing destination")
	}
	if _, err := moduleLvmPvMoveData(context.Background(), conn, map[string]any{"destination": "/dev/sdc"}); err == nil {
		t.Fatal("want error for missing source")
	}
}
