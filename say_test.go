package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSayBasic(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v say": {RC: 0},
		"say hello":      {RC: 0},
	})
	res, err := moduleSay(context.Background(), conn, map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed || res.Msg != "hello" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSayVoice(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v say":      {RC: 0},
		"say hello -v Zarvox": {RC: 0},
	})
	res, err := moduleSay(context.Background(), conn, map[string]any{"msg": "hello", "voice": "Zarvox"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSayFallsBackToEspeak(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v say":    {RC: 1},
		"command -v espeak": {RC: 0},
		"espeak hello":      {RC: 0},
	})
	res, err := moduleSay(context.Background(), conn, map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSayNoneFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v say":       {RC: 1},
		"command -v espeak":    {RC: 1},
		"command -v espeak-ng": {RC: 1},
	})
	res, err := moduleSay(context.Background(), conn, map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSayMissingMsg(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSay(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing msg")
	}
}
