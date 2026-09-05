package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const circonusCreateCmd = `CIRCONUS_API_TOKEN=XXXXXXXXXXXXXXXXX circli -object annotation -call create ` +
	`-where '{"category":"This category groups like annotations","description":"This is a detailed description of the config change","start":1395940006,"stop":1395954407,"title":"App Config Change"}'`

func TestModuleCirconusAnnotationCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v circli": {RC: 0},
		circonusCreateCmd:   {RC: 0, Stdout: `{"_cid":"/annotation/100000"}`},
	})
	res, err := moduleCirconusAnnotation(context.Background(), conn, map[string]any{
		"api_key":     "XXXXXXXXXXXXXXXXX",
		"title":       "App Config Change",
		"description": "This is a detailed description of the config change",
		"category":    "This category groups like annotations",
		"start":       1395940006,
		"stop":        1395954407,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed; res = %+v", res)
	}
	if len(conn.Commands) != 2 || conn.Commands[1] != circonusCreateCmd {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleCirconusAnnotationDefaultsStopFromDuration(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v circli": {RC: 0},
	})
	res, err := moduleCirconusAnnotation(context.Background(), conn, map[string]any{
		"api_key":     "XXXXXXXXXXXXXXXXX",
		"title":       "t",
		"description": "d",
		"category":    "c",
		"start":       1000,
		"duration":    300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if !strings.Contains(conn.Commands[1], `"stop":1300`) {
		t.Fatalf("expected stop = start+duration, command: %s", conn.Commands[1])
	}
}

func TestModuleCirconusAnnotationFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v circli": {RC: 0},
		circonusCreateCmd:   {RC: 1, Stderr: "401 Unauthorized"},
	})
	res, err := moduleCirconusAnnotation(context.Background(), conn, map[string]any{
		"api_key":     "XXXXXXXXXXXXXXXXX",
		"title":       "App Config Change",
		"description": "This is a detailed description of the config change",
		"category":    "This category groups like annotations",
		"start":       1395940006,
		"stop":        1395954407,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleCirconusAnnotationMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v circli": {RC: 1},
	})
	res, err := moduleCirconusAnnotation(context.Background(), conn, map[string]any{
		"api_key":     "x",
		"title":       "t",
		"description": "d",
		"category":    "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleCirconusAnnotationMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleCirconusAnnotation(context.Background(), conn, map[string]any{"api_key": "x"})
	if err == nil {
		t.Fatal("want error for missing required args")
	}
}
