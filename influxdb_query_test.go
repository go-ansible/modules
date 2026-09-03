package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleInfluxdbQuery(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -database mydb -format json -execute 'select mean(value) from connections'": {
			RC: 0, Stdout: `{"results":[{"series":[{"name":"connections","columns":["time","mean"],"values":[["1970-01-01T00:00:00Z",1245.5]]}]}]}`,
		},
	})
	res, err := moduleInfluxdbQuery(context.Background(), conn, map[string]any{
		"database_name": "mydb", "query": "select mean(value) from connections",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	rows, ok := res.Extra["query_results"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("query_results = %#v", res.Extra["query_results"])
	}
	if rows[0]["mean"] != 1245.5 {
		t.Fatalf("mean = %v", rows[0]["mean"])
	}
}

func TestModuleInfluxdbQueryEmptyResults(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -database mydb -format json -execute 'select mean(value) from connections'": {
			RC: 0, Stdout: `{"results":[{}]}`,
		},
	})
	res, err := moduleInfluxdbQuery(context.Background(), conn, map[string]any{
		"database_name": "mydb", "query": "select mean(value) from connections",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := res.Extra["query_results"].([]map[string]any)
	if !ok || len(rows) != 0 {
		t.Fatalf("query_results = %#v, want empty non-nil slice", res.Extra["query_results"])
	}
}

func TestModuleInfluxdbQueryServerError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -database mydb -format json -execute 'bad query'": {
			RC: 0, Stdout: `{"results":[{"error":"error parsing query"}]}`,
		},
	})
	res, err := moduleInfluxdbQuery(context.Background(), conn, map[string]any{
		"database_name": "mydb", "query": "bad query",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a server-side query error")
	}
}

func TestModuleInfluxdbQueryMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleInfluxdbQuery(context.Background(), conn, map[string]any{"database_name": "mydb"}); err == nil {
		t.Fatal("want error for missing query")
	}
	if _, err := moduleInfluxdbQuery(context.Background(), conn, map[string]any{"query": "select 1"}); err == nil {
		t.Fatal("want error for missing database_name")
	}
}
