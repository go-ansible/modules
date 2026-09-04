package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOdbcQuery(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"isql -b -k mydsn": {RC: 0, Stdout: "1\n\n1 rows fetched\n"},
	})
	res, err := moduleOdbc(context.Background(), conn, map[string]any{
		"dsn": "mydsn", "query": "SELECT 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if conn.Stdins[0] != "SELECT 1\n" {
		t.Fatalf("stdin = %q", conn.Stdins[0])
	}
}

func TestModuleOdbcParamSubstitution(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"isql -b -k mydsn": {RC: 0},
	})
	_, err := moduleOdbc(context.Background(), conn, map[string]any{
		"dsn": "mydsn", "query": "SELECT * FROM t WHERE a=? AND b=?",
		"params": []any{"x", "y's"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT * FROM t WHERE a='x' AND b='y''s'\n"
	if conn.Stdins[0] != want {
		t.Fatalf("stdin = %q, want %q", conn.Stdins[0], want)
	}
}

func TestModuleOdbcFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"isql -b -k mydsn": {RC: 1, Stderr: "connection refused"},
	})
	res, err := moduleOdbc(context.Background(), conn, map[string]any{
		"dsn": "mydsn", "query": "SELECT 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleOdbcMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOdbc(context.Background(), conn, map[string]any{"dsn": "mydsn"}); err == nil {
		t.Fatal("want error for missing query")
	}
}
