package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePauseSeconds(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sleep 5": {RC: 0},
	})
	res, err := modulePause(context.Background(), conn, map[string]any{"seconds": 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePauseMinutes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sleep 120": {RC: 0},
	})
	res, err := modulePause(context.Background(), conn, map[string]any{"minutes": 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePauseCombined(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sleep 65": {RC: 0},
	})
	res, err := modulePause(context.Background(), conn, map[string]any{"seconds": 5, "minutes": 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePauseNoDurationFailsHonestly(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := modulePause(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: no interactive prompt support")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no commands run, got %v", conn.Commands)
	}
}
