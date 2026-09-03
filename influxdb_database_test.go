package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleInfluxdbDatabaseCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'SHOW DATABASES'": {
			RC: 0, Stdout: `{"results":[{"series":[{"name":"databases","columns":["name"],"values":[["_internal"]]}]}]}`,
		},
		`INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'CREATE DATABASE "mydb"'`: {
			RC: 0, Stdout: `{"results":[{}]}`,
		},
	})
	res, err := moduleInfluxdbDatabase(context.Background(), conn, map[string]any{"database_name": "mydb"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbDatabaseAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'SHOW DATABASES'": {
			RC: 0, Stdout: `{"results":[{"series":[{"name":"databases","columns":["name"],"values":[["mydb"]]}]}]}`,
		},
	})
	res, err := moduleInfluxdbDatabase(context.Background(), conn, map[string]any{"database_name": "mydb"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbDatabaseDrop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'SHOW DATABASES'": {
			RC: 0, Stdout: `{"results":[{"series":[{"name":"databases","columns":["name"],"values":[["mydb"]]}]}]}`,
		},
		`INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'DROP DATABASE "mydb"'`: {
			RC: 0, Stdout: `{"results":[{}]}`,
		},
	})
	res, err := moduleInfluxdbDatabase(context.Background(), conn, map[string]any{"database_name": "mydb", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbDatabaseDropAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'SHOW DATABASES'": {
			RC: 0, Stdout: `{"results":[{"series":[{"name":"databases","columns":["name"],"values":[]}]}]}`,
		},
	})
	res, err := moduleInfluxdbDatabase(context.Background(), conn, map[string]any{"database_name": "mydb", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbDatabaseServerError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'SHOW DATABASES'": {
			RC: 1, Stderr: "connection refused",
		},
	})
	if _, err := moduleInfluxdbDatabase(context.Background(), conn, map[string]any{"database_name": "mydb"}); err == nil {
		t.Fatal("want a Go error when the connection fails")
	}
}

func TestModuleInfluxdbDatabaseMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleInfluxdbDatabase(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing database_name")
	}
}
