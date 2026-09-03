package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleInfluxdbWrite(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -database mydb -format json -execute 'INSERT connections,host=server01,region=us-west value=2000i'": {
			RC: 0, Stdout: `{"results":[{}]}`,
		},
	})
	res, err := moduleInfluxdbWrite(context.Background(), conn, map[string]any{
		"database_name": "mydb",
		"data_points": []any{
			map[string]any{
				"measurement": "connections",
				"tags":        map[string]any{"host": "server01", "region": "us-west"},
				"fields":      map[string]any{"value": 2000},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbWriteMultiplePoints(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -database mydb -format json -execute 'INSERT a value=1i'": {RC: 0, Stdout: `{"results":[{}]}`},
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -database mydb -format json -execute 'INSERT b value=2i'": {RC: 0, Stdout: `{"results":[{}]}`},
	})
	res, err := moduleInfluxdbWrite(context.Background(), conn, map[string]any{
		"database_name": "mydb",
		"data_points": []any{
			map[string]any{"measurement": "a", "fields": map[string]any{"value": 1}},
			map[string]any{"measurement": "b", "fields": map[string]any{"value": 2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v, want 2 INSERT calls", conn.Commands)
	}
}

func TestModuleInfluxdbWriteFailurePartway(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -database mydb -format json -execute 'INSERT a value=1i'": {RC: 0, Stdout: `{"results":[{}]}`},
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -database mydb -format json -execute 'INSERT b value=2i'": {RC: 1, Stderr: "write failed"},
	})
	res, err := moduleInfluxdbWrite(context.Background(), conn, map[string]any{
		"database_name": "mydb",
		"data_points": []any{
			map[string]any{"measurement": "a", "fields": map[string]any{"value": 1}},
			map[string]any{"measurement": "b", "fields": map[string]any{"value": 2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when a later point's write fails")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v, want the first point already attempted", conn.Commands)
	}
}

func TestModuleInfluxdbWriteMissingFields(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleInfluxdbWrite(context.Background(), conn, map[string]any{
		"database_name": "mydb",
		"data_points":   []any{map[string]any{"measurement": "a"}},
	}); err == nil {
		t.Fatal("want error for a data point missing fields")
	}
}

func TestModuleInfluxdbWriteMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleInfluxdbWrite(context.Background(), conn, map[string]any{"database_name": "mydb"}); err == nil {
		t.Fatal("want error for missing data_points")
	}
	if _, err := moduleInfluxdbWrite(context.Background(), conn, map[string]any{
		"data_points": []any{map[string]any{"measurement": "a", "fields": map[string]any{"value": 1}}},
	}); err == nil {
		t.Fatal("want error for missing database_name")
	}
}

func TestInfluxLineProtocolEscaping(t *testing.T) {
	line, err := influxLineProtocol(map[string]any{
		"measurement": "my measurement",
		"tags":        map[string]any{"tag key": "tag,val"},
		"fields":      map[string]any{"msg": "hello \"world\"", "n": 3.5, "ok": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `my\ measurement,tag\ key=tag\,val msg="hello \"world\"",n=3.5,ok=t`
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
}
